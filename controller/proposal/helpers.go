package proposal

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"text/template"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.tmpl"))

func renderTemplate(name string, data any) string {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Sprintf("(template %q error: %v)", name, err)
	}
	return buf.String()
}

const (
	proposalFinalizer  = "agentic.openshift.io/execution-rbac-cleanup"
	defaultMaxAttempts = 3

	reasonAnalysisInProgress     = "AnalysisInProgress"
	reasonAnalysisComplete       = "AnalysisComplete"
	reasonAnalysisFailed         = "AnalysisFailed"
	reasonExecutionInProgress    = "ExecutionInProgress"
	reasonExecutionComplete      = "ExecutionComplete"
	reasonExecutionFailed        = "ExecutionFailed"
	reasonExecutionSkipped       = "ExecutionSkipped"
	reasonVerificationInProgress = "VerificationInProgress"
	reasonVerificationPassed     = "VerificationPassed"
	reasonVerificationFailed     = "VerificationFailed"
	reasonVerificationSkipped    = "VerificationSkipped"
	reasonUserApproved           = "UserApproved"
	reasonWorkflowFailed         = "WorkflowResolutionFailed"
	reasonMaxAttemptsReached     = "MaxAttemptsReached"
	reasonAwaitingSync           = "AwaitingSync"
	defaultSandboxSA             = "lightspeed-agent"
	reasonRevisionAnalyzing      = "RevisionAnalyzing"
	reasonRevisionComplete       = "RevisionComplete"
)

var stepFailReasons = map[string]string{
	agenticv1alpha1.ProposalConditionAnalyzed: reasonAnalysisFailed,
	agenticv1alpha1.ProposalConditionExecuted: reasonExecutionFailed,
	agenticv1alpha1.ProposalConditionVerified: reasonVerificationFailed,
}

func (r *ProposalReconciler) failStep(ctx context.Context, log logr.Logger, proposal *agenticv1alpha1.Proposal, conditionType string, err error) (ctrl.Result, error) {
	log.Error(err, "step failed", "condition", conditionType)
	base := proposal.DeepCopy()
	proposal.Status.Phase = agenticv1alpha1.ProposalPhaseFailed
	completedAt := metav1.Now()

	switch conditionType {
	case agenticv1alpha1.ProposalConditionAnalyzed:
		ensureAnalysisStep(proposal)
		proposal.Status.Steps.Analysis.CompletionTime = &completedAt
	case agenticv1alpha1.ProposalConditionExecuted:
		ensureExecutionStep(proposal)
		proposal.Status.Steps.Execution.CompletionTime = &completedAt
	case agenticv1alpha1.ProposalConditionVerified:
		ensureVerificationStep(proposal)
		proposal.Status.Steps.Verification.CompletionTime = &completedAt
	}

	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionFalse,
		Reason:  stepFailReasons[conditionType],
		Message: err.Error(),
	})
	if statusErr := r.statusPatch(ctx, proposal, base); statusErr != nil {
		log.Error(statusErr, "failed to patch status after step failure")
	}
	return ctrl.Result{Requeue: true}, nil
}

func (r *ProposalReconciler) statusPatch(ctx context.Context, proposal *agenticv1alpha1.Proposal, base *agenticv1alpha1.Proposal) error {
	return r.Status().Patch(ctx, proposal, client.MergeFrom(base))
}

func isTerminal(phase agenticv1alpha1.ProposalPhase) bool {
	switch phase {
	case agenticv1alpha1.ProposalPhaseCompleted, agenticv1alpha1.ProposalPhaseDenied, agenticv1alpha1.ProposalPhaseEscalated:
		return true
	}
	return false
}

func ensureAnalysisStep(proposal *agenticv1alpha1.Proposal) {
	if proposal.Status.Steps.Analysis == nil {
		proposal.Status.Steps.Analysis = &agenticv1alpha1.AnalysisStepStatus{}
	}
}

func ensureExecutionStep(proposal *agenticv1alpha1.Proposal) {
	if proposal.Status.Steps.Execution == nil {
		proposal.Status.Steps.Execution = &agenticv1alpha1.ExecutionStepStatus{}
	}
}

func ensureVerificationStep(proposal *agenticv1alpha1.Proposal) {
	if proposal.Status.Steps.Verification == nil {
		proposal.Status.Steps.Verification = &agenticv1alpha1.VerificationStepStatus{}
	}
}

func setVerificationSkipped(proposal *agenticv1alpha1.Proposal) {
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionVerified,
		Status:  metav1.ConditionTrue,
		Reason:  reasonVerificationSkipped,
		Message: "Verification step not configured in workflow",
	})
}

func resultName(proposal *agenticv1alpha1.Proposal, step string) string {
	return fmt.Sprintf("%s-%s-%d", proposal.Name, step, *proposal.Status.Attempt)
}

func (r *ProposalReconciler) selectedOption(ctx context.Context, proposal *agenticv1alpha1.Proposal) (*agenticv1alpha1.RemediationOption, error) {
	analysis := proposal.Status.Steps.Analysis
	if analysis == nil || analysis.SelectedOption == nil || analysis.Result == nil {
		return nil, nil
	}
	result, err := r.Content.GetAnalysisResult(ctx, analysis.Result.Name)
	if err != nil {
		return nil, err
	}
	idx := int(*analysis.SelectedOption)
	if idx < 0 || idx >= len(result.Options) {
		return nil, fmt.Errorf("selectedOption index %d out of range (have %d options)", idx, len(result.Options))
	}
	return &result.Options[idx], nil
}

func determineFailure(proposal *agenticv1alpha1.Proposal) (*agenticv1alpha1.SandboxStep, *string) {
	for _, c := range proposal.Status.Conditions {
		if c.Status != metav1.ConditionFalse {
			continue
		}
		var step agenticv1alpha1.SandboxStep
		switch c.Type {
		case agenticv1alpha1.ProposalConditionAnalyzed:
			step = agenticv1alpha1.SandboxStepAnalysis
		case agenticv1alpha1.ProposalConditionExecuted:
			step = agenticv1alpha1.SandboxStepExecution
		case agenticv1alpha1.ProposalConditionVerified:
			step = agenticv1alpha1.SandboxStepVerification
		default:
			continue
		}
		msg := c.Message
		return &step, &msg
	}
	return nil, nil
}

func attemptAlreadyRecorded(attempts []agenticv1alpha1.PreviousAttempt, num int32) bool {
	for _, a := range attempts {
		if a.Attempt == num {
			return true
		}
	}
	return false
}

func (r *ProposalReconciler) maxAttempts(proposal *agenticv1alpha1.Proposal) int {
	if proposal.Spec.MaxAttempts != nil {
		return int(*proposal.Spec.MaxAttempts)
	}
	return defaultMaxAttempts
}

type escalationData struct {
	Name             string
	RequestName      string
	AttemptCount     int32
	PreviousAttempts []escalationAttempt
}

type escalationAttempt struct {
	Attempt       int32
	FailedStep    string
	FailureReason string
}

func buildEscalationRequest(proposal *agenticv1alpha1.Proposal) string {
	data := escalationData{
		Name:         proposal.Name,
		RequestName:  proposal.Spec.Request.Name,
		AttemptCount: *proposal.Status.Attempt,
	}
	for _, pa := range proposal.Status.PreviousAttempts {
		a := escalationAttempt{Attempt: pa.Attempt}
		if pa.FailedStep != nil {
			a.FailedStep = string(*pa.FailedStep)
		}
		if pa.FailureReason != nil {
			a.FailureReason = *pa.FailureReason
		}
		data.PreviousAttempts = append(data.PreviousAttempts, a)
	}
	return renderTemplate("escalation_request.tmpl", data)
}

func needsRevision(proposal *agenticv1alpha1.Proposal) bool {
	if proposal.Spec.Revision == nil || *proposal.Spec.Revision <= 0 {
		return false
	}
	analysis := proposal.Status.Steps.Analysis
	if analysis == nil {
		return true
	}
	if analysis.ObservedRevision == nil {
		return true
	}
	return *proposal.Spec.Revision > *analysis.ObservedRevision
}

func revisionResultName(proposal *agenticv1alpha1.Proposal) string {
	return fmt.Sprintf("%s-analysis-%d-rev%d", proposal.Name, *proposal.Status.Attempt, *proposal.Spec.Revision)
}

func revisionRequestName(proposal *agenticv1alpha1.Proposal) string {
	return fmt.Sprintf("%s-revision-%d", proposal.Name, *proposal.Spec.Revision)
}

type revisionData struct {
	Revision            int32
	ProposalName        string
	Namespace           string
	PreviousResultName  string
	RevisionRequestName string
}

func buildRevisionContext(proposal *agenticv1alpha1.Proposal) string {
	data := revisionData{
		Revision:            *proposal.Spec.Revision,
		ProposalName:        proposal.Name,
		Namespace:           proposal.Namespace,
		RevisionRequestName: revisionRequestName(proposal),
	}
	if proposal.Status.Steps.Analysis != nil && proposal.Status.Steps.Analysis.Result != nil {
		data.PreviousResultName = proposal.Status.Steps.Analysis.Result.Name
	}
	return renderTemplate("revision_context.tmpl", data)
}
