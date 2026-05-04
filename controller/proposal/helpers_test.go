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
