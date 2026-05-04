package proposal

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"reflect"
	"text/template"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
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
	rbacCleanupFinalizer  = "agentic.openshift.io/execution-rbac-cleanup"
	defaultMaxAttempts = 0

	reasonInProgress        = "InProgress"
	reasonComplete          = "Complete"
	reasonFailed            = "Failed"
	reasonSkipped           = "Skipped"
	reasonPassed            = "Passed"
	reasonWorkflowFailed    = "WorkflowResolutionFailed"
	reasonPendingApproval   = "PendingApproval"
	reasonAutoApproved      = "AutoApproved"
	reasonUserDenied        = "UserDenied"
	defaultSandboxSA        = "lightspeed-agent"
	reasonRevising          = "Revising"
	reasonRevisionComplete  = "RevisionComplete"
	reasonRetryingExecution = agenticv1alpha1.ReasonRetryingExecution
	reasonRetriesExhausted  = agenticv1alpha1.ReasonRetriesExhausted
)


func (r *ProposalReconciler) failStep(ctx context.Context, log logr.Logger, proposal *agenticv1alpha1.Proposal, conditionType string, err error) (ctrl.Result, error) {
	log.Error(err, "step failed", "condition", conditionType)
	base := proposal.DeepCopy()
	completedAt := metav1.Now()

	switch conditionType {
	case agenticv1alpha1.ProposalConditionAnalyzed:
		proposal.Status.Steps.Analysis.CompletionTime = &completedAt
	case agenticv1alpha1.ProposalConditionExecuted:
		proposal.Status.Steps.Execution.CompletionTime = &completedAt
	case agenticv1alpha1.ProposalConditionVerified:
		proposal.Status.Steps.Verification.CompletionTime = &completedAt
	case agenticv1alpha1.ProposalConditionEscalated:
		proposal.Status.Steps.Escalation.CompletionTime = &completedAt
	}

	var crName string
	var createErr error
	switch conditionType {
	case agenticv1alpha1.ProposalConditionAnalyzed:
		crName, createErr = r.createAnalysisResult(ctx, proposal, nil, proposal.Status.Steps.Analysis.Sandbox, proposal.Status.Steps.Analysis.StartTime, &completedAt, err.Error())
		if createErr == nil {
			proposal.Status.Steps.Analysis.Results = append(proposal.Status.Steps.Analysis.Results, agenticv1alpha1.StepResultRef{Name: crName, Success: false})
		}
	case agenticv1alpha1.ProposalConditionExecuted:
		crName, createErr = r.createExecutionResult(ctx, proposal, nil, proposal.Status.Steps.Execution.Sandbox, proposal.Status.Steps.Execution.StartTime, &completedAt, err.Error())
		if createErr == nil {
			proposal.Status.Steps.Execution.Results = append(proposal.Status.Steps.Execution.Results, agenticv1alpha1.StepResultRef{Name: crName, Success: false})
		}
	case agenticv1alpha1.ProposalConditionVerified:
		crName, createErr = r.createVerificationResult(ctx, proposal, nil, proposal.Status.Steps.Verification.Sandbox, proposal.Status.Steps.Verification.StartTime, &completedAt, err.Error())
		if createErr == nil {
			proposal.Status.Steps.Verification.Results = append(proposal.Status.Steps.Verification.Results, agenticv1alpha1.StepResultRef{Name: crName, Success: false})
		}
	case agenticv1alpha1.ProposalConditionEscalated:
		crName, createErr = r.createEscalationResult(ctx, proposal, nil, proposal.Status.Steps.Escalation.Sandbox, proposal.Status.Steps.Escalation.StartTime, &completedAt, err.Error())
		if createErr == nil {
			proposal.Status.Steps.Escalation.Results = append(proposal.Status.Steps.Escalation.Results, agenticv1alpha1.StepResultRef{Name: crName, Success: false})
		}
	}
	if createErr != nil {
		log.Error(createErr, "failed to create failure result CR")
	}

	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionFalse,
		Reason:  reasonFailed,
		Message: err.Error(),
	})
	if statusErr := r.statusPatch(ctx, proposal, base); statusErr != nil {
		log.Error(statusErr, "failed to patch status after step failure")
	}
	return ctrl.Result{}, nil
}

func (r *ProposalReconciler) statusPatch(ctx context.Context, proposal *agenticv1alpha1.Proposal, base *agenticv1alpha1.Proposal) error {
	return r.Status().Patch(ctx, proposal, client.MergeFrom(base))
}

func hasSandboxClaims(proposal *agenticv1alpha1.Proposal) bool {
	return proposal.Status.Steps.Analysis.Sandbox.ClaimName != "" ||
		proposal.Status.Steps.Execution.Sandbox.ClaimName != "" ||
		proposal.Status.Steps.Verification.Sandbox.ClaimName != "" ||
		proposal.Status.Steps.Escalation.Sandbox.ClaimName != ""
}

func isTerminal(phase agenticv1alpha1.ProposalPhase) bool {
	switch phase {
	case agenticv1alpha1.ProposalPhaseCompleted, agenticv1alpha1.ProposalPhaseDenied, agenticv1alpha1.ProposalPhaseEscalated:
		return true
	}
	return false
}

func setVerificationSkipped(proposal *agenticv1alpha1.Proposal) {
	meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionVerified,
		Status:  metav1.ConditionTrue,
		Reason:  reasonSkipped,
		Message: "Verification step not configured in workflow",
	})
}

func (r *ProposalReconciler) selectedOption(ctx context.Context, proposal *agenticv1alpha1.Proposal) (*agenticv1alpha1.RemediationOption, error) {
	analysis := proposal.Status.Steps.Analysis
	if analysis.SelectedOption == nil || len(analysis.Results) == 0 {
		return nil, nil
	}
	latestRef := analysis.Results[len(analysis.Results)-1]
	var result agenticv1alpha1.AnalysisResult
	if err := r.Get(ctx, types.NamespacedName{Name: latestRef.Name, Namespace: proposal.Namespace}, &result); err != nil {
		return nil, fmt.Errorf("get AnalysisResult %s: %w", latestRef.Name, err)
	}
	idx := int(*analysis.SelectedOption)
	if idx < 0 || idx >= len(result.Options) {
		r.Log.Info("selectedOption index out of range", "index", idx, "options", len(result.Options), "proposal", proposal.Name)
		return nil, nil
	}
	return &result.Options[idx], nil
}

func resetExecutionAndVerification(steps *agenticv1alpha1.StepsStatus) {
	steps.Execution.StartTime = nil
	steps.Execution.CompletionTime = nil
	steps.Execution.Sandbox = agenticv1alpha1.SandboxInfo{}
	steps.Verification.StartTime = nil
	steps.Verification.CompletionTime = nil
	steps.Verification.Sandbox = agenticv1alpha1.SandboxInfo{}
}

func maxAttempts(proposal *agenticv1alpha1.Proposal) int {
	if proposal.Spec.MaxAttempts != nil {
		return int(*proposal.Spec.MaxAttempts)
	}
	return defaultMaxAttempts
}

type escalationData struct {
	Name                string
	Namespace           string
	Request             string
	AttemptCount        int32
	AnalysisResults     []agenticv1alpha1.StepResultRef
	ExecutionResults    []agenticv1alpha1.StepResultRef
	VerificationResults []agenticv1alpha1.StepResultRef
}

func buildEscalationRequest(proposal *agenticv1alpha1.Proposal) string {
	data := escalationData{
		Name:                proposal.Name,
		Namespace:           proposal.Namespace,
		Request:             proposal.Spec.Request,
		AttemptCount:        *proposal.Status.Attempts,
		AnalysisResults:     proposal.Status.Steps.Analysis.Results,
		ExecutionResults:    proposal.Status.Steps.Execution.Results,
		VerificationResults: proposal.Status.Steps.Verification.Results,
	}
	return renderTemplate("escalation_request.tmpl", data)
}

func needsRevision(proposal *agenticv1alpha1.Proposal) bool {
	if proposal.Spec.Revision == nil || *proposal.Spec.Revision <= 0 {
		return false
	}
	analysis := proposal.Status.Steps.Analysis
	if analysis.ObservedRevision == nil {
		return true
	}
	return *proposal.Spec.Revision > *analysis.ObservedRevision
}

type revisionData struct {
	Revision     int32
	ProposalName string
	Namespace    string
	Feedback     string
}

func buildRevisionContext(proposal *agenticv1alpha1.Proposal) string {
	data := revisionData{
		Revision:     *proposal.Spec.Revision,
		ProposalName: proposal.Name,
		Namespace:    proposal.Namespace,
		Feedback:     proposal.Spec.RevisionFeedback,
	}
	return renderTemplate("revision_context.tmpl", data)
}

func prettyJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return "{}"
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

type analysisQuery struct {
	Request string
}

func buildAnalysisQuery(requestText string) string {
	return renderTemplate("analysis_query.tmpl", analysisQuery{Request: requestText})
}

type executionQuery struct {
	OptionJSON string
}

func buildExecutionQuery(option *agenticv1alpha1.RemediationOption) string {
	return renderTemplate("execution_query.tmpl", executionQuery{OptionJSON: prettyJSON(option)})
}

type verificationQuery struct {
	OptionJSON    string
	ExecutionJSON string
}

func buildVerificationQuery(option *agenticv1alpha1.RemediationOption, exec *ExecutionOutput) string {
	return renderTemplate("verification_query.tmpl", verificationQuery{
		OptionJSON:    prettyJSON(option),
		ExecutionJSON: prettyJSON(executionOutputToAgentResult(exec)),
	})
}
