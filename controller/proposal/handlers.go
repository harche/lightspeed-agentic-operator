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

// handleAnalysis checks approval for the analysis step and runs it.
func (r *ProposalReconciler) handleAnalysis(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
	resolved *resolvedWorkflow,
	approval *agenticv1alpha1.ProposalApproval,
	policy *agenticv1alpha1.ApprovalPolicy,
) (ctrl.Result, error) {
	log.Info("handling analysis", "attempts", *proposal.Status.Attempts)

	if isStageDenied(approval, agenticv1alpha1.SandboxStepAnalysis) {
		return r.denyProposal(ctx, log, proposal, "Analysis denied by user")
	}

	if !isStageApproved(approval, policy, agenticv1alpha1.SandboxStepAnalysis) {
		log.Info("analysis pending approval")
		return ctrl.Result{}, nil
	}

	analyzed := meta.FindStatusCondition(proposal.Status.Conditions, agenticv1alpha1.ProposalConditionAnalyzed)
	if analyzed != nil {
		if analyzed.Status == metav1.ConditionUnknown {
			log.Info("analysis already in progress, waiting")
			return ctrl.Result{}, nil
		}
		if analyzed.Status == metav1.ConditionTrue {
			log.Info("analysis already completed")
			return ctrl.Result{}, nil
		}
	}

	base := proposal.DeepCopy()
	now := metav1.Now()
	proposal.Status.Steps.Analysis.StartTime = &now
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionAnalyzed,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonInProgress,
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
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionAnalyzed,
		Status:  metav1.ConditionTrue,
		Reason:  reasonComplete,
		Message: fmt.Sprintf("Analysis complete with %d option(s)", len(analysisResult.Options)),
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update after analysis: %w", err)
	}

	log.Info("analysis complete", "options", len(analysisResult.Options))
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

	analyzed := meta.FindStatusCondition(proposal.Status.Conditions, agenticv1alpha1.ProposalConditionAnalyzed)
	if analyzed != nil && analyzed.Status == metav1.ConditionUnknown {
		log.Info("revision already in progress, waiting")
		return ctrl.Result{}, nil
	}

	base := proposal.DeepCopy()
	meta.RemoveStatusCondition(&proposal.Status.Conditions, agenticv1alpha1.ProposalConditionExecuted)
	meta.RemoveStatusCondition(&proposal.Status.Conditions, agenticv1alpha1.ProposalConditionVerified)
	now := metav1.Now()
	proposal.Status.Steps.Analysis.StartTime = &now
	proposal.Status.Steps.Analysis.CompletionTime = nil
	proposal.Status.Steps.Analysis.SelectedOption = nil
	resetExecutionAndVerification(&proposal.Status.Steps)
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionAnalyzed,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonRevising,
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
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionAnalyzed,
		Status:  metav1.ConditionTrue,
		Reason:  reasonRevisionComplete,
		Message: fmt.Sprintf("Revision %d complete with %d option(s)", revision, len(analysisResult.Options)),
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update after revision: %w", err)
	}

	log.Info("revision analysis complete", "revision", revision, "options", len(analysisResult.Options))
	return ctrl.Result{}, nil
}

// handleExecution checks approval and runs execution (or skips if not configured).
func (r *ProposalReconciler) handleExecution(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
	resolved *resolvedWorkflow,
	approval *agenticv1alpha1.ProposalApproval,
	policy *agenticv1alpha1.ApprovalPolicy,
) (ctrl.Result, error) {
	log.Info("handling execution")

	if resolved.Execution == nil {
		base := proposal.DeepCopy()
		meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
			Type:    agenticv1alpha1.ProposalConditionExecuted,
			Status:  metav1.ConditionTrue,
			Reason:  reasonSkipped,
			Message: "Execution step not configured",
		})

		if resolved.Verification == nil {
			setVerificationSkipped(proposal)
			if err := r.statusPatch(ctx, proposal, base); err != nil {
				return ctrl.Result{}, fmt.Errorf("update to Completed (advisory): %w", err)
			}
			log.Info("advisory-only — completed")
			return ctrl.Result{}, nil
		}

		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, fmt.Errorf("update after execution skip: %w", err)
		}
		return ctrl.Result{}, nil
	}

	if isStageDenied(approval, agenticv1alpha1.SandboxStepExecution) {
		return r.denyProposal(ctx, log, proposal, "Execution denied by user")
	}

	if !isStageApproved(approval, policy, agenticv1alpha1.SandboxStepExecution) {
		log.Info("execution pending approval")
		return ctrl.Result{}, nil
	}

	executed := meta.FindStatusCondition(proposal.Status.Conditions, agenticv1alpha1.ProposalConditionExecuted)
	if executed != nil {
		if executed.Status == metav1.ConditionUnknown {
			log.Info("execution already in progress, waiting")
			return ctrl.Result{}, nil
		}
		if executed.Status == metav1.ConditionTrue {
			log.Info("execution already completed")
			return ctrl.Result{}, nil
		}
	}

	// Copy selected option from ProposalApproval and prune non-selected options.
	// Skip if already pruned (e.g., on retry after verification failure).
	if proposal.Status.Steps.Analysis.SelectedOption == nil {
		base := proposal.DeepCopy()
		option := getStageOption(approval)
		if option != nil {
			proposal.Status.Steps.Analysis.SelectedOption = option
		}
		pruneToSelectedOption(&proposal.Status.Steps.Analysis)
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, fmt.Errorf("persist selected option: %w", err)
		}
	}

	selectedOption := r.selectedOption(proposal)

	base := proposal.DeepCopy()
	if selectedOption != nil && (len(selectedOption.RBAC.NamespaceScoped) > 0 || len(selectedOption.RBAC.ClusterScoped) > 0) {
		if err := ensureExecutionRBAC(ctx, r.Client, proposal, &selectedOption.RBAC, defaultSandboxSA, proposal.Namespace); err != nil {
			return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionExecuted, fmt.Errorf("ensure execution RBAC: %w", err))
		}
		if err := r.Patch(ctx, proposal, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, fmt.Errorf("persist RBAC annotation: %w", err)
		}
		base = proposal.DeepCopy()
	}

	meta.RemoveStatusCondition(&proposal.Status.Conditions, agenticv1alpha1.ProposalConditionVerified)
	now := metav1.Now()
	proposal.Status.Steps.Execution.StartTime = &now
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionExecuted,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonInProgress,
		Message: "Execution agent is running",
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Executing: %w", err)
	}

	execResult, err := r.Agent.Execute(ctx, proposal, *resolved.Execution, selectedOption)
	if err != nil {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionExecuted, err)
	}
	if !execResult.Success {
		return r.failStep(ctx, log, proposal, agenticv1alpha1.ProposalConditionExecuted, fmt.Errorf("execution agent reported failure"))
	}

	base = proposal.DeepCopy()
	completedAt := metav1.Now()
	proposal.Status.Steps.Execution.CompletionTime = &completedAt
	applyExecutionResult(&proposal.Status.Steps.Execution, execResult)
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionExecuted,
		Status:  metav1.ConditionTrue,
		Reason:  reasonComplete,
		Message: "Execution completed",
	})

	if resolved.Verification == nil {
		setVerificationSkipped(proposal)
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, fmt.Errorf("update to Completed (trust-mode): %w", err)
		}
		log.Info("execution complete, verification skipped")
		return ctrl.Result{}, nil
	}

	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Verifying: %w", err)
	}

	log.Info("execution complete, verifying")
	return ctrl.Result{}, nil
}

// handleVerification checks approval and runs verification.
func (r *ProposalReconciler) handleVerification(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
	resolved *resolvedWorkflow,
	approval *agenticv1alpha1.ProposalApproval,
	policy *agenticv1alpha1.ApprovalPolicy,
) (ctrl.Result, error) {
	log.Info("verifying")

	base := proposal.DeepCopy()

	if resolved.Verification == nil {
		setVerificationSkipped(proposal)
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if isStageDenied(approval, agenticv1alpha1.SandboxStepVerification) {
		return r.denyProposal(ctx, log, proposal, "Verification denied by user")
	}

	if !isStageApproved(approval, policy, agenticv1alpha1.SandboxStepVerification) {
		log.Info("verification pending approval")
		return ctrl.Result{}, nil
	}

	verified := meta.FindStatusCondition(proposal.Status.Conditions, agenticv1alpha1.ProposalConditionVerified)
	if verified != nil && verified.Status == metav1.ConditionUnknown {
		log.Info("verification already in progress, waiting")
		return ctrl.Result{}, nil
	}

	now := metav1.Now()
	proposal.Status.Steps.Verification.StartTime = &now
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionVerified,
		Status:  metav1.ConditionUnknown,
		Reason:  reasonInProgress,
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

	allPassed := verifyResult.Success
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
		maxRetries := maxAttempts(proposal)

		if int(retryCount) < maxRetries {
			next := retryCount + 1
			log.Info("verification failed, retrying execution", "retryCount", next, "maxRetries", maxRetries, "summary", verifyResult.Summary)
			proposal.Status.Steps.Execution.RetryCount = &next
			resetExecutionAndVerification(&proposal.Status.Steps)
			meta.RemoveStatusCondition(&proposal.Status.Conditions, agenticv1alpha1.ProposalConditionExecuted)
			meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
				Type:    agenticv1alpha1.ProposalConditionVerified,
				Status:  metav1.ConditionFalse,
				Reason:  reasonRetryingExecution,
				Message: fmt.Sprintf("Verification failed (attempt %d/%d): %s", next, maxRetries, verifyResult.Summary),
			})
			if err := r.statusPatch(ctx, proposal, base); err != nil {
				return ctrl.Result{}, fmt.Errorf("update for execution retry: %w", err)
			}
			return ctrl.Result{}, nil
		}

		log.Info("verification retries exhausted, returning to analysis", "retryCount", retryCount, "summary", verifyResult.Summary)
		proposal.Status.Steps.Analysis.SelectedOption = nil
		meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
			Type:    agenticv1alpha1.ProposalConditionVerified,
			Status:  metav1.ConditionFalse,
			Reason:  reasonRetriesExhausted,
			Message: fmt.Sprintf("Verification failed after %d attempt(s): %s", retryCount, verifyResult.Summary),
		})
		if err := r.statusPatch(ctx, proposal, base); err != nil {
			return ctrl.Result{}, fmt.Errorf("update (retries exhausted): %w", err)
		}
		return ctrl.Result{}, nil
	}

	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionVerified,
		Status:  metav1.ConditionTrue,
		Reason:  reasonPassed,
		Message: verifyResult.Summary,
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Completed: %w", err)
	}

	log.Info("verification passed", "summary", verifyResult.Summary)
	return ctrl.Result{}, nil
}

// handleFailed performs cleanup for system failures.
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

	currentAttempt := *proposal.Status.Attempts
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
				LabelParent: proposal.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "agentic.openshift.io/v1alpha1",
				Kind:       "Proposal",
				Name:       proposal.Name,
				UID:        proposal.UID,
			}},
		},
		Spec: agenticv1alpha1.ProposalSpec{
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

// denyProposal transitions the proposal to Denied (terminal).
func (r *ProposalReconciler) denyProposal(
	ctx context.Context,
	log logr.Logger,
	proposal *agenticv1alpha1.Proposal,
	message string,
) (ctrl.Result, error) {
	log.Info("denying proposal", "message", message)
	base := proposal.DeepCopy()
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionDenied,
		Status:  metav1.ConditionTrue,
		Reason:  reasonUserDenied,
		Message: message,
	})
	if err := r.statusPatch(ctx, proposal, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("update to Denied: %w", err)
	}
	return ctrl.Result{}, nil
}
