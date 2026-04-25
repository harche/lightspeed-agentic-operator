package proposal

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

// handlePending runs analysis and transitions to Proposed.
func (r *ProposalReconciler) handlePending(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
	resolved *resolvedWorkflow,
) (ctrl.Result, error) {
	log.Info("handling pending", "attempt", *proposal.Status.Attempt)

	base := proposal.DeepCopy()
	proposal.Status.Phase = agenticv1alpha1.ProposalPhaseAnalyzing
	now := metav1.Now()
	ensureAnalysisStep(proposal)
	proposal.Status.Steps.Analysis.StartTime = &now
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionAnalyzed,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonAnalysisInProgress,
		Message: "Analysis agent is running",
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Analyzing: %w", err)
	}

	reqContent, err := r.Content.GetRequestContent(ctx, proposal.Spec.Request.Name)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionAnalyzed, fmt.Errorf("read request content %q: %w", proposal.Spec.Request.Name, err))
	}

	analysisResult, err := r.Agent.Analyze(ctx, proposal, resolved.Analysis, reqContent)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionAnalyzed, err)
	}

	resultName := resultName(proposal, "analysis")
	if err := r.Content.CreateAnalysisResult(ctx, resultName, *analysisResult); err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionAnalyzed, fmt.Errorf("store analysis result: %w", err))
	}

	base = proposal.DeepCopy()
	completedAt := metav1.Now()
	proposal.Status.Steps.Analysis.CompletionTime = &completedAt
	proposal.Status.Steps.Analysis.Result = &agenticv1alpha1.ContentReference{Name: resultName}
	proposal.Status.Phase = agenticv1alpha1.ProposalPhaseProposed
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionAnalyzed,
		Status:  metav1.ConditionTrue,
		Reason:  reasonAnalysisComplete,
		Message: fmt.Sprintf("Analysis complete with %d option(s)", len(analysisResult.Options)),
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Proposed: %w", err)
	}

	log.Info("analysis complete, awaiting approval", "options", len(analysisResult.Options))
	return ctrl.Result{}, nil
}

// handleApproved runs execution (or skips to AwaitingSync/Completed).
func (r *ProposalReconciler) handleApproved(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
	resolved *resolvedWorkflow,
) (ctrl.Result, error) {
	log.Info("handling approved")

	base := proposal.DeepCopy()
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionApproved,
		Status:  metav1.ConditionTrue,
		Reason:  reasonUserApproved,
		Message: "Proposal approved by user",
	})

	selectedOption, err := r.selectedOption(ctx, proposal)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionExecuted, fmt.Errorf("read selected option: %w", err))
	}

	if resolved.Execution == nil {
		meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
			Type:    agenticv1alpha1.ProposalConditionExecuted,
			Status:  metav1.ConditionTrue,
			Reason:  reasonExecutionSkipped,
			Message: "Execution step not configured in workflow",
		})

		if resolved.Verification == nil {
			proposal.Status.Phase = agenticv1alpha1.ProposalPhaseCompleted
			setVerificationSkipped(proposal)
			if err := r.statusPatch(ctx, proposal, base); err != nil {
				return ctrl.Result{}, fmt.Errorf("update to Completed (advisory): %w", err)
			}
			log.Info("advisory-only — completed")
			return ctrl.Result{}, nil
		}

		proposal.Status.Phase = agenticv1alpha1.ProposalPhaseAwaitingSync
		meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
			Type:    agenticv1alpha1.ProposalConditionAwaitingSync,
			Status:  metav1.ConditionTrue,
			Reason:  reasonAwaitingSync,
			Message: "Waiting for external application before verification",
		})
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, fmt.Errorf("update to AwaitingSync: %w", err)
		}
		log.Info("awaiting external sync")
		return ctrl.Result{}, nil
	}

	if selectedOption != nil && selectedOption.RBAC != nil {
		// TODO: resolve sandbox SA from template when sandbox infra is wired
		sandboxSA := "lightspeed-agent"
		operatorNS := proposal.Namespace
		if err := ensureExecutionRBAC(ctx, r.Client, proposal, selectedOption.RBAC, sandboxSA, operatorNS); err != nil {
			return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionExecuted, fmt.Errorf("ensure execution RBAC: %w", err))
		}
	}

	proposal.Status.Phase = agenticv1alpha1.ProposalPhaseExecuting
	now := metav1.Now()
	ensureExecutionStep(proposal)
	proposal.Status.Steps.Execution.StartTime = &now
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionExecuted,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonExecutionInProgress,
		Message: "Execution agent is running",
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Executing: %w", err)
	}

	execResult, err := r.Agent.Execute(ctx, proposal, *resolved.Execution, selectedOption)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionExecuted, err)
	}

	resultName := resultName(proposal, "execution")
	if err := r.Content.CreateExecutionResult(ctx, resultName, *execResult); err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionExecuted, fmt.Errorf("store execution result: %w", err))
	}

	base = proposal.DeepCopy()
	completedAt := metav1.Now()
	proposal.Status.Steps.Execution.CompletionTime = &completedAt
	proposal.Status.Steps.Execution.Result = &agenticv1alpha1.ContentReference{Name: resultName}
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionExecuted,
		Status:  metav1.ConditionTrue,
		Reason:  reasonExecutionComplete,
		Message: "Execution completed",
	})

	if resolved.Verification == nil {
		proposal.Status.Phase = agenticv1alpha1.ProposalPhaseCompleted
		setVerificationSkipped(proposal)
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, fmt.Errorf("update to Completed (trust-mode): %w", err)
		}
		log.Info("execution complete, verification skipped")
		return ctrl.Result{}, nil
	}

	proposal.Status.Phase = agenticv1alpha1.ProposalPhaseVerifying
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Verifying: %w", err)
	}

	log.Info("execution complete, verifying")
	return ctrl.Result{Requeue: true}, nil
}

// handleVerifying runs the verification agent.
func (r *ProposalReconciler) handleVerifying(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
	resolved *resolvedWorkflow,
) (ctrl.Result, error) {
	log.Info("verifying")

	base := proposal.DeepCopy()

	if resolved.Verification == nil {
		proposal.Status.Phase = agenticv1alpha1.ProposalPhaseCompleted
		setVerificationSkipped(proposal)
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	now := metav1.Now()
	ensureVerificationStep(proposal)
	proposal.Status.Steps.Verification.StartTime = &now
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionVerified,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonVerificationInProgress,
		Message: "Verification agent is running",
	})

	selectedOption, err := r.selectedOption(ctx, proposal)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionVerified, fmt.Errorf("read selected option for verification: %w", err))
	}

	var execResult *agenticv1alpha1.ExecutionResultSpec
	if proposal.Status.Steps.Execution != nil && proposal.Status.Steps.Execution.Result != nil {
		execResult, err = r.Content.GetExecutionResult(ctx, proposal.Status.Steps.Execution.Result.Name)
		if err != nil {
			log.Error(err, "could not read execution result for verification context, continuing")
		}
	}

	verifyResult, err := r.Agent.Verify(ctx, proposal, *resolved.Verification, selectedOption, execResult)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionVerified, err)
	}

	resultName := resultName(proposal, "verification")
	if err := r.Content.CreateVerificationResult(ctx, resultName, *verifyResult); err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionVerified, fmt.Errorf("store verification result: %w", err))
	}

	base = proposal.DeepCopy()
	completedAt := metav1.Now()
	proposal.Status.Steps.Verification.CompletionTime = &completedAt
	proposal.Status.Steps.Verification.Result = &agenticv1alpha1.ContentReference{Name: resultName}

	allPassed := true
	for _, check := range verifyResult.Checks {
		if check.Passed == nil || !*check.Passed {
			allPassed = false
			break
		}
	}

	if !allPassed {
		log.Info("verification checks failed", "summary", verifyResult.Summary)
		proposal.Status.Phase = agenticv1alpha1.ProposalPhaseFailed
		meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
			Type:    agenticv1alpha1.ProposalConditionVerified,
			Status:  metav1.ConditionFalse,
			Reason:  reasonVerificationFailed,
			Message: verifyResult.Summary,
		})
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	proposal.Status.Phase = agenticv1alpha1.ProposalPhaseCompleted
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionVerified,
		Status:  metav1.ConditionTrue,
		Reason:  reasonVerificationPassed,
		Message: verifyResult.Summary,
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Completed: %w", err)
	}

	log.Info("verification passed", "summary", verifyResult.Summary)
	return ctrl.Result{}, nil
}

// handleFailed captures the failure, then retries or escalates.
func (r *ProposalReconciler) handleFailed(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
) (ctrl.Result, error) {
	if proposal.Annotations[rbacNamespacesAnnotation] != "" {
		if err := cleanupExecutionRBAC(ctx, r.Client, proposal); err != nil {
			log.Error(err, "RBAC cleanup on failure")
		}
	}

	maxAttempts := r.maxAttempts(proposal)
	log.Info("handling failure", "attempt", *proposal.Status.Attempt, "maxAttempts", maxAttempts)

	base := proposal.DeepCopy()
	currentAttempt := *proposal.Status.Attempt
	if !attemptAlreadyRecorded(proposal.Status.PreviousAttempts, currentAttempt) {
		failedStep, failureReason := determineFailure(proposal)
		proposal.Status.PreviousAttempts = append(proposal.Status.PreviousAttempts, agenticv1alpha1.PreviousAttempt{
			Attempt:       currentAttempt,
			FailedStep:    failedStep,
			FailureReason: failureReason,
		})
	}

	if int(currentAttempt) >= maxAttempts {
		log.Info("max attempts reached, escalating")
		proposal.Status.Phase = agenticv1alpha1.ProposalPhaseEscalated
		meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
			Type:    agenticv1alpha1.ProposalConditionEscalated,
			Status:  metav1.ConditionTrue,
			Reason:  reasonMaxAttemptsReached,
			Message: fmt.Sprintf("Failed after %d attempt(s)", currentAttempt),
		})
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, fmt.Errorf("update to Escalated: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	nextAttempt := currentAttempt + 1
	proposal.Status.Attempt = &nextAttempt
	proposal.Status.Phase = agenticv1alpha1.ProposalPhasePending
	proposal.Status.Steps = &agenticv1alpha1.StepsStatus{}
	proposal.Status.Conditions = nil
	log.Info("retrying", "nextAttempt", nextAttempt)

	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update for retry: %w", err)
	}
	return ctrl.Result{Requeue: true}, nil
}

// handleEscalated creates a child escalation proposal.
func (r *ProposalReconciler) handleEscalated(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
) (ctrl.Result, error) {
	childName := truncateK8sName(proposal.Name + "-escalation")

	escalationText := buildEscalationRequest(proposal)
	requestName := childName + "-request"
	if err := r.Content.CreateRequestContent(ctx, requestName, agenticv1alpha1.RequestContentSpec{
		ContentPayload: agenticv1alpha1.ContentPayload{
			MediaType: "text/plain",
			Content:   escalationText,
		},
	}); err != nil {
		log.Error(err, "create escalation request content (may already exist)")
	}

	child := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{
			Name:      childName,
			Namespace: proposal.Namespace,
			Labels: map[string]string{
				labelParent: proposal.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "agentic.openshift.io/v1alpha1",
				Kind:       "Proposal",
				Name:       proposal.Name,
				UID:        proposal.UID,
			}},
		},
		Spec: agenticv1alpha1.ProposalSpec{
			WorkflowRef:      proposal.Spec.WorkflowRef,
			Request:          agenticv1alpha1.ContentReference{Name: requestName},
			ParentRef:        &corev1.LocalObjectReference{Name: proposal.Name},
			TargetNamespaces: proposal.Spec.TargetNamespaces,
		},
	}

	if err := r.Create(ctx, child); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("create escalation proposal: %w", err)
	}

	log.Info("created escalation child", "child", childName)
	return ctrl.Result{}, nil
}
