package proposal

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

const (
	LabelAttempt = "agentic.openshift.io/attempt"
)

func resultCRName(proposalName, step string, index int) string {
	return truncateK8sName(fmt.Sprintf("%s-%s-%d", proposalName, step, index))
}

func proposalOwnerRef(proposal *agenticv1alpha1.Proposal) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         "agentic.openshift.io/v1alpha1",
		Kind:               "Proposal",
		Name:               proposal.Name,
		UID:                proposal.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

func resultLabels(proposalName, step string, attempt int32) map[string]string {
	return map[string]string{
		LabelProposal: proposalName,
		LabelStep:     step,
		LabelAttempt:  strconv.Itoa(int(attempt)),
	}
}

func proposalAttempt(proposal *agenticv1alpha1.Proposal) int32 {
	if proposal.Status.Attempts != nil {
		return *proposal.Status.Attempts
	}
	return 1
}

func executionRetryIndex(proposal *agenticv1alpha1.Proposal) int32 {
	if proposal.Status.Steps.Execution.RetryCount != nil {
		return *proposal.Status.Steps.Execution.RetryCount
	}
	return 0
}

func (r *ProposalReconciler) createAnalysisResult(
	ctx context.Context,
	proposal *agenticv1alpha1.Proposal,
	result *AnalysisOutput,
	sandbox agenticv1alpha1.SandboxInfo,
	startTime *metav1.Time,
	completionTime *metav1.Time,
	failureReason string,
) (string, error) {
	crName := resultCRName(proposal.Name, "analysis", len(proposal.Status.Steps.Analysis.Results)+1)
	attempt := proposalAttempt(proposal)

	cr := &agenticv1alpha1.AnalysisResult{
		ObjectMeta: metav1.ObjectMeta{
			Name:            crName,
			Namespace:       proposal.Namespace,
			Labels:          resultLabels(proposal.Name, "analysis", attempt),
			OwnerReferences: []metav1.OwnerReference{proposalOwnerRef(proposal)},
		},
		ProposalName:   proposal.Name,
		Attempt:        attempt,
		Sandbox:        sandbox,
		StartTime:      startTime,
		CompletionTime: completionTime,
		FailureReason:  failureReason,
	}

	if result != nil {
		cr.Success = result.Success
		cr.Options = result.Options
		cr.Components = result.Components
	}

	return crName, createIdempotent(ctx, r.Client, cr, "AnalysisResult")
}

func (r *ProposalReconciler) createExecutionResult(
	ctx context.Context,
	proposal *agenticv1alpha1.Proposal,
	result *ExecutionOutput,
	sandbox agenticv1alpha1.SandboxInfo,
	startTime *metav1.Time,
	completionTime *metav1.Time,
	failureReason string,
) (string, error) {
	crName := resultCRName(proposal.Name, "execution", len(proposal.Status.Steps.Execution.Results)+1)
	attempt := proposalAttempt(proposal)

	cr := &agenticv1alpha1.ExecutionResult{
		ObjectMeta: metav1.ObjectMeta{
			Name:            crName,
			Namespace:       proposal.Namespace,
			Labels:          resultLabels(proposal.Name, "execution", attempt),
			OwnerReferences: []metav1.OwnerReference{proposalOwnerRef(proposal)},
		},
		ProposalName:   proposal.Name,
		Attempt:        attempt,
		RetryIndex:     executionRetryIndex(proposal),
		Sandbox:        sandbox,
		StartTime:      startTime,
		CompletionTime: completionTime,
		FailureReason:  failureReason,
	}

	if result != nil {
		cr.Success = result.Success
		cr.ActionsTaken = result.ActionsTaken
		cr.Verification = result.Verification
		cr.Components = result.Components
	}

	return crName, createIdempotent(ctx, r.Client, cr, "ExecutionResult")
}

func (r *ProposalReconciler) createVerificationResult(
	ctx context.Context,
	proposal *agenticv1alpha1.Proposal,
	result *VerificationOutput,
	sandbox agenticv1alpha1.SandboxInfo,
	startTime *metav1.Time,
	completionTime *metav1.Time,
	failureReason string,
) (string, error) {
	crName := resultCRName(proposal.Name, "verification", len(proposal.Status.Steps.Verification.Results)+1)
	attempt := proposalAttempt(proposal)

	cr := &agenticv1alpha1.VerificationResult{
		ObjectMeta: metav1.ObjectMeta{
			Name:            crName,
			Namespace:       proposal.Namespace,
			Labels:          resultLabels(proposal.Name, "verification", attempt),
			OwnerReferences: []metav1.OwnerReference{proposalOwnerRef(proposal)},
		},
		ProposalName:   proposal.Name,
		Attempt:        attempt,
		RetryIndex:     executionRetryIndex(proposal),
		Sandbox:        sandbox,
		StartTime:      startTime,
		CompletionTime: completionTime,
		FailureReason:  failureReason,
	}

	if result != nil {
		cr.Success = result.Success
		cr.Checks = result.Checks
		cr.Summary = result.Summary
		cr.Components = result.Components
	}

	return crName, createIdempotent(ctx, r.Client, cr, "VerificationResult")
}

func (r *ProposalReconciler) createEscalationResult(
	ctx context.Context,
	proposal *agenticv1alpha1.Proposal,
	result *EscalationOutput,
	sandbox agenticv1alpha1.SandboxInfo,
	startTime *metav1.Time,
	completionTime *metav1.Time,
	failureReason string,
) (string, error) {
	crName := resultCRName(proposal.Name, "escalation", len(proposal.Status.Steps.Escalation.Results)+1)
	attempt := proposalAttempt(proposal)

	cr := &agenticv1alpha1.EscalationResult{
		ObjectMeta: metav1.ObjectMeta{
			Name:            crName,
			Namespace:       proposal.Namespace,
			Labels:          resultLabels(proposal.Name, "escalation", attempt),
			OwnerReferences: []metav1.OwnerReference{proposalOwnerRef(proposal)},
		},
		ProposalName:   proposal.Name,
		Attempt:        attempt,
		Sandbox:        sandbox,
		StartTime:      startTime,
		CompletionTime: completionTime,
		FailureReason:  failureReason,
	}

	if result != nil {
		cr.Success = result.Success
		cr.Summary = result.Summary
		cr.Content = result.Content
	}

	return crName, createIdempotent(ctx, r.Client, cr, "EscalationResult")
}

func createIdempotent(ctx context.Context, c client.Client, obj client.Object, kind string) error {
	if err := c.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("create %s %s: %w", kind, obj.GetName(), err)
	}
	return nil
}
