package proposal

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

func ptr32(v int32) *int32 { return &v }

func TestSelectedOption_FromAnalysisResult(t *testing.T) {
	scheme := testScheme()

	proposal := &agenticv1alpha1.Proposal{}
	proposal.Name = "test"
	proposal.Namespace = "default"
	proposal.Status.Steps.Analysis.SelectedOption = ptr32(2)
	proposal.Status.Steps.Analysis.Results = []agenticv1alpha1.StepResultRef{
		{Name: "test-analysis-1", Outcome: agenticv1alpha1.ActionOutcomeSucceeded},
	}

	// Create an AnalysisResult CR with the options
	analysisResult := &agenticv1alpha1.AnalysisResult{}
	analysisResult.Name = "test-analysis-1"
	analysisResult.Namespace = "default"
	analysisResult.Options = []agenticv1alpha1.RemediationOption{
		{Title: "A"},
		{Title: "B"},
		{Title: "C"},
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(analysisResult).Build()
	r := &ProposalReconciler{Client: fc, Log: logr.Discard()}

	got, err := r.selectedOption(context.Background(), proposal)
	if err != nil {
		t.Fatalf("selectedOption() error: %v", err)
	}
	if got == nil {
		t.Fatal("selectedOption() returned nil")
	}
	if got.Title != "C" {
		t.Errorf("selectedOption().Title = %q, want %q", got.Title, "C")
	}
}

func TestSelectedOption_NilSelected(t *testing.T) {
	scheme := testScheme()

	proposal := &agenticv1alpha1.Proposal{}
	proposal.Name = "test"
	proposal.Namespace = "default"
	// SelectedOption is nil

	fc := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ProposalReconciler{Client: fc, Log: logr.Discard()}

	got, err := r.selectedOption(context.Background(), proposal)
	if err != nil {
		t.Fatalf("selectedOption() error: %v", err)
	}
	if got != nil {
		t.Errorf("selectedOption() should return nil when SelectedOption is nil, got %+v", got)
	}
}

func TestMaxAttempts(t *testing.T) {
	makeApproval := func(maxAttempts int32) *agenticv1alpha1.ProposalApproval {
		return &agenticv1alpha1.ProposalApproval{
			Spec: agenticv1alpha1.ProposalApprovalSpec{
				Stages: []agenticv1alpha1.ApprovalStage{
					{
						Type:      agenticv1alpha1.ApprovalStageExecution,
						Execution: &agenticv1alpha1.ExecutionApproval{MaxAttempts: maxAttempts},
					},
				},
			},
		}
	}
	makePolicy := func(maxAttempts int32) *agenticv1alpha1.ApprovalPolicy {
		return &agenticv1alpha1.ApprovalPolicy{
			Spec: agenticv1alpha1.ApprovalPolicySpec{MaxAttempts: maxAttempts},
		}
	}

	tests := []struct {
		name     string
		approval *agenticv1alpha1.ProposalApproval
		policy   *agenticv1alpha1.ApprovalPolicy
		want     int
	}{
		{name: "nil approval and nil policy defaults to 1", want: 1},
		{name: "nil approval with policy=3 uses policy", policy: makePolicy(3), want: 3},
		{name: "admin sets 3, user picks 1 → operator uses 1", approval: makeApproval(1), policy: makePolicy(3), want: 1},
		{name: "admin sets 3, user picks 2 → operator uses 2", approval: makeApproval(2), policy: makePolicy(3), want: 2},
		{name: "admin sets 3, user picks 3 → operator uses 3", approval: makeApproval(3), policy: makePolicy(3), want: 3},
		{name: "user exceeds admin ceiling → capped to ceiling", approval: makeApproval(3), policy: makePolicy(1), want: 1},
		{name: "user sets maxAttempts, no policy → capped to default 1", approval: makeApproval(3), want: 1},
		{name: "approval without execution stage → uses policy", approval: &agenticv1alpha1.ProposalApproval{}, policy: makePolicy(2), want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxAttempts(tt.approval, tt.policy)
			if got != tt.want {
				t.Errorf("maxAttempts() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSelectedOption_OutOfRange(t *testing.T) {
	scheme := testScheme()

	proposal := &agenticv1alpha1.Proposal{}
	proposal.Name = "test"
	proposal.Namespace = "default"
	proposal.Status.Steps.Analysis.SelectedOption = ptr32(5)
	proposal.Status.Steps.Analysis.Results = []agenticv1alpha1.StepResultRef{
		{Name: "test-analysis-1", Outcome: agenticv1alpha1.ActionOutcomeSucceeded},
	}

	analysisResult := &agenticv1alpha1.AnalysisResult{}
	analysisResult.Name = "test-analysis-1"
	analysisResult.Namespace = "default"
	analysisResult.Options = []agenticv1alpha1.RemediationOption{
		{Title: "A"},
		{Title: "B"},
		{Title: "C"},
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(analysisResult).Build()
	r := &ProposalReconciler{Client: fc, Log: logr.Discard()}

	got, err := r.selectedOption(context.Background(), proposal)
	if err != nil {
		t.Fatalf("selectedOption() error: %v", err)
	}
	if got != nil {
		t.Errorf("selectedOption() should return nil for out-of-range index, got %+v", got)
	}
}
