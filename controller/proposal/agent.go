package proposal

import (
	"context"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

// AgentCaller abstracts the agent invocation path. The reconciler
// passes structured data; the implementation decides how to format
// it for the LLM (text-only prompt vs multimodal with binary
// attachments). In production this manages sandbox lifecycle + HTTP
// calls; in tests a stub returns canned results.
type AgentCaller interface {
	Analyze(ctx context.Context, proposal *agenticv1alpha1.Proposal, step resolvedStep, request *agenticv1alpha1.RequestContentSpec) (*agenticv1alpha1.AnalysisResultSpec, error)
	Execute(ctx context.Context, proposal *agenticv1alpha1.Proposal, step resolvedStep, option *agenticv1alpha1.RemediationOption) (*agenticv1alpha1.ExecutionResultSpec, error)
	Verify(ctx context.Context, proposal *agenticv1alpha1.Proposal, step resolvedStep, option *agenticv1alpha1.RemediationOption, exec *agenticv1alpha1.ExecutionResultSpec) (*agenticv1alpha1.VerificationResultSpec, error)
}

// StubAgentCaller returns canned success results. Wire in a real
// implementation (sandbox + HTTP) when the agent infrastructure is ready.
type StubAgentCaller struct{}

func (s *StubAgentCaller) Analyze(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RequestContentSpec) (*agenticv1alpha1.AnalysisResultSpec, error) {
	reversible := true
	return &agenticv1alpha1.AnalysisResultSpec{
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
				Reversible:  &reversible,
			},
		}},
	}, nil
}

func (s *StubAgentCaller) Execute(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RemediationOption) (*agenticv1alpha1.ExecutionResultSpec, error) {
	success := true
	improved := true
	return &agenticv1alpha1.ExecutionResultSpec{
		ActionsTaken: []agenticv1alpha1.ExecutionAction{{
			Type:        "stub",
			Description: "Stub execution action",
			Success:     &success,
		}},
		Verification: &agenticv1alpha1.ExecutionVerification{
			ConditionImproved: &improved,
			Summary:           "Stub inline verification passed",
		},
	}, nil
}

func (s *StubAgentCaller) Verify(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RemediationOption, _ *agenticv1alpha1.ExecutionResultSpec) (*agenticv1alpha1.VerificationResultSpec, error) {
	passed := true
	return &agenticv1alpha1.VerificationResultSpec{
		Checks: []agenticv1alpha1.VerifyCheck{{
			Name:   "stub-check",
			Source: "stub",
			Value:  "ok",
			Passed: &passed,
		}},
		Summary: "Stub verification passed",
	}, nil
}
