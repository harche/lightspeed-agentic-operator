package proposal

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
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

	analysisResult, err := r.Agent.Analyze(ctx, proposal, resolved.Analysis, proposal.Spec.Request)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionAnalyzed, err)
	}

	base = proposal.DeepCopy()
	completedAt := metav1.Now()
	proposal.Status.Steps.Analysis.CompletionTime = &completedAt
	applyAnalysisResult(&proposal.Status.Steps.Analysis, analysisResult)
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

// handleRevision re-runs analysis with revision context appended to the
// agent's system prompt.
func (r *ProposalReconciler) handleRevision(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
	resolved *resolvedWorkflow,
) (ctrl.Result, error) {
	revision := *proposal.Spec.Revision
	log.Info("handling revision", "revision", revision)

	base := proposal.DeepCopy()
	proposal.Status.Phase = agenticv1alpha1.ProposalPhaseAnalyzing
	now := metav1.Now()
	proposal.Status.Steps.Analysis.StartTime = &now
	proposal.Status.Steps.Analysis.CompletionTime = nil
	proposal.Status.Steps.Analysis.SelectedOption = nil
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionAnalyzed,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonRevisionAnalyzing,
		Message: fmt.Sprintf("Re-analyzing for revision %d", revision),
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Analyzing (revision): %w", err)
	}

	revisionSuffix := buildRevisionContext(proposal)
	requestWithRevision := proposal.Spec.Request + "\n\n" + revisionSuffix

	analysisResult, err := r.Agent.Analyze(ctx, proposal, resolved.Analysis, requestWithRevision)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionAnalyzed, err)
	}

	base = proposal.DeepCopy()
	completedAt := metav1.Now()
	proposal.Status.Steps.Analysis.CompletionTime = &completedAt
	applyAnalysisResult(&proposal.Status.Steps.Analysis, analysisResult)
	proposal.Status.Steps.Analysis.ObservedRevision = &revision
	proposal.Status.Phase = agenticv1alpha1.ProposalPhaseProposed
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionAnalyzed,
		Status:  metav1.ConditionTrue,
		Reason:  reasonRevisionComplete,
		Message: fmt.Sprintf("Revision %d complete with %d option(s)", revision, len(analysisResult.Options)),
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Proposed (revision): %w", err)
	}

	log.Info("revision analysis complete", "revision", revision, "options", len(analysisResult.Options))
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

	selectedOption := r.selectedOption(proposal)

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

	if selectedOption != nil && (len(selectedOption.RBAC.NamespaceScoped) > 0 || len(selectedOption.RBAC.ClusterScoped) > 0) {
		if err := ensureExecutionRBAC(ctx, r.Client, proposal, &selectedOption.RBAC, defaultSandboxSA, proposal.Namespace); err != nil {
			return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionExecuted, fmt.Errorf("ensure execution RBAC: %w", err))
		}
		if err := r.Patch(ctx, proposal, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, fmt.Errorf("persist RBAC annotation: %w", err)
		}
		base = proposal.DeepCopy()
	}

	proposal.Status.Phase = agenticv1alpha1.ProposalPhaseExecuting
	now := metav1.Now()
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

	base = proposal.DeepCopy()
	completedAt := metav1.Now()
	proposal.Status.Steps.Execution.CompletionTime = &completedAt
	applyExecutionResult(&proposal.Status.Steps.Execution, execResult)
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
	proposal.Status.Steps.Verification.StartTime = &now
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionVerified,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonVerificationInProgress,
		Message: "Verification agent is running",
	})

	selectedOption := r.selectedOption(proposal)

	var execOutput *ExecutionOutput
	if len(proposal.Status.Steps.Execution.ActionsTaken) > 0 {
		execOutput = &ExecutionOutput{
			ActionsTaken: proposal.Status.Steps.Execution.ActionsTaken,
			Verification: proposal.Status.Steps.Execution.Verification,
			Components:   proposal.Status.Steps.Execution.Components,
		}
	}

	verifyResult, err := r.Agent.Verify(ctx, proposal, *resolved.Verification, selectedOption, execOutput)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionVerified, err)
	}

	base = proposal.DeepCopy()
	completedAt := metav1.Now()
	proposal.Status.Steps.Verification.CompletionTime = &completedAt
	applyVerificationResult(&proposal.Status.Steps.Verification, verifyResult)

	allPassed := true
	for _, check := range verifyResult.Checks {
		if check.Result != agenticv1alpha1.CheckResultPassed {
			allPassed = false
			break
		}
	}

	if !allPassed {
		retryCount := int32(0)
		if proposal.Status.Steps.Execution.RetryCount != nil {
			retryCount = *proposal.Status.Steps.Execution.RetryCount
		}
		maxRetries := maxAttempts(proposal, resolved)

		if int(retryCount) < maxRetries {
			next := retryCount + 1
			log.Info("verification failed, retrying execution", "retryCount", next, "maxRetries", maxRetries, "summary", verifyResult.Summary)
			proposal.Status.Steps.Execution.RetryCount = &next
			resetExecutionAndVerification(&proposal.Status.Steps)
			proposal.Status.Phase = agenticv1alpha1.ProposalPhaseApproved
			meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
				Type:    agenticv1alpha1.ProposalConditionVerified,
				Status:  metav1.ConditionFalse,
				Reason:  reasonRetryingExecution,
				Message: fmt.Sprintf("Verification failed (attempt %d/%d): %s", next, maxRetries, verifyResult.Summary),
			})
			if err := r.statusPatch(ctx, proposal, base); err != nil {
				return ctrl.Result{}, fmt.Errorf("update for execution retry: %w", err)
			}
			return ctrl.Result{Requeue: true}, nil
		}

		log.Info("verification retries exhausted, returning to Proposed", "retryCount", retryCount, "summary", verifyResult.Summary)
		proposal.Status.Steps.Analysis.SelectedOption = nil
		proposal.Status.Phase = agenticv1alpha1.ProposalPhaseProposed
		meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
			Type:    agenticv1alpha1.ProposalConditionVerified,
			Status:  metav1.ConditionFalse,
			Reason:  reasonRetriesExhausted,
			Message: fmt.Sprintf("Verification failed after %d attempt(s): %s", retryCount, verifyResult.Summary),
		})
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, fmt.Errorf("update to Proposed (retries exhausted): %w", err)
		}
		return ctrl.Result{}, nil
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

// handleFailed performs cleanup for system failures. Failed is a terminal
// phase — system failures (agent errors, sandbox crashes, network issues)
// are not retryable. Objective failures (verification checks not passed)
// are handled inline in handleVerifying with their own retry loop.
func (r *ProposalReconciler) handleFailed(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
) (ctrl.Result, error) {
	log.Info("handling system failure (terminal)")

	if proposal.Annotations[rbacNamespacesAnnotation] != "" {
		if err := cleanupExecutionRBAC(ctx, r.Client, proposal); err != nil {
			log.Error(err, "RBAC cleanup on failure")
		}
	}

	currentAttempt := *proposal.Status.Attempt
	if !attemptAlreadyRecorded(proposal.Status.PreviousAttempts, currentAttempt) {
		base := proposal.DeepCopy()
		failedStep, failureReason := determineFailure(proposal)
		proposal.Status.PreviousAttempts = append(proposal.Status.PreviousAttempts, agenticv1alpha1.PreviousAttempt{
			Attempt:       currentAttempt,
			FailedStep:    failedStep,
			FailureReason: failureReason,
		})
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, fmt.Errorf("record system failure: %w", err)
		}
	}
	return ctrl.Result{}, nil
}

// handleEscalated creates a child escalation proposal.
func (r *ProposalReconciler) handleEscalated(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
) (ctrl.Result, error) {
	childName := truncateK8sName(proposal.Name + "-escalation")

	escalationText := buildEscalationRequest(proposal)

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
			TemplateRef:      proposal.Spec.TemplateRef,
			Tools:            proposal.Spec.Tools,
			Analysis:         proposal.Spec.Analysis,
			Execution:        proposal.Spec.Execution,
			Verification:     proposal.Spec.Verification,
			Request:          escalationText,
			Parent:           agenticv1alpha1.ProposalReference{Name: proposal.Name},
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
