package proposal

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

// AnalysisOutput holds the analysis agent's output.
type AnalysisOutput struct {
	Options    []agenticv1alpha1.RemediationOption
	Components []apiextensionsv1.JSON
}

// ExecutionOutput holds the execution agent's output.
type ExecutionOutput struct {
	ActionsTaken []agenticv1alpha1.ExecutionAction
	Verification agenticv1alpha1.ExecutionVerification
	Components   []apiextensionsv1.JSON
}

// VerificationOutput holds the verification agent's output.
type VerificationOutput struct {
	Checks     []agenticv1alpha1.VerifyCheck
	Summary    string
	Components []apiextensionsv1.JSON
}

// AgentCaller abstracts the agent invocation path. The reconciler
// passes structured data; the implementation decides how to format
// it for the LLM (text-only prompt vs multimodal with binary
// attachments). In production this manages sandbox lifecycle + HTTP
// calls; in tests a stub returns canned results.
//
// HTTP implementations should POST to a single /query endpoint with
// a "phase" field in the request body rather than separate endpoints.
type AgentCaller interface {
	Analyze(ctx context.Context, proposal *agenticv1alpha1.Proposal, step resolvedStep, requestText string) (*AnalysisOutput, error)
	Execute(ctx context.Context, proposal *agenticv1alpha1.Proposal, step resolvedStep, option *agenticv1alpha1.RemediationOption) (*ExecutionOutput, error)
	Verify(ctx context.Context, proposal *agenticv1alpha1.Proposal, step resolvedStep, option *agenticv1alpha1.RemediationOption, exec *ExecutionOutput) (*VerificationOutput, error)
}

// StubAgentCaller returns canned success results. Wire in a real
// implementation (sandbox + HTTP) when the agent infrastructure is ready.
type StubAgentCaller struct{}

func (s *StubAgentCaller) Analyze(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ string) (*AnalysisOutput, error) {
	return &AnalysisOutput{
		Options: []agenticv1alpha1.RemediationOption{{
			Title: "Stub remediation",
			Diagnosis: agenticv1alpha1.DiagnosisResult{
				Summary:    "Stub diagnosis",
				Confidence: "Medium",
				RootCause:  "Stub root cause",
			},
			Proposal: agenticv1alpha1.ProposalResult{
				Description: "Stub proposal",
				Actions:     []agenticv1alpha1.ProposedAction{{Type: "stub", Description: "Stub action"}},
				Risk:        "Low",
				Reversible:  agenticv1alpha1.ReversibilityReversible,
			},
		}},
	}, nil
}

func (s *StubAgentCaller) Execute(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RemediationOption) (*ExecutionOutput, error) {
	return &ExecutionOutput{
		ActionsTaken: []agenticv1alpha1.ExecutionAction{{
			Type:        "stub",
			Description: "Stub execution action",
			Outcome:     agenticv1alpha1.ActionOutcomeSucceeded,
		}},
		Verification: agenticv1alpha1.ExecutionVerification{
			ConditionOutcome: agenticv1alpha1.ConditionOutcomeImproved,
			Summary:          "Stub inline verification passed",
		},
	}, nil
}

func (s *StubAgentCaller) Verify(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RemediationOption, _ *ExecutionOutput) (*VerificationOutput, error) {
	return &VerificationOutput{
		Checks: []agenticv1alpha1.VerifyCheck{{
			Name:   "stub-check",
			Source: "stub",
			Value:  "ok",
			Result: agenticv1alpha1.CheckResultPassed,
		}},
		Summary: "Stub verification passed",
	}, nil
}
