package proposal

import (
	"context"
	"strings"
	"testing"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestApprove_Success(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseProposed)
	p.Status.Steps.Analysis = agenticv1alpha1.AnalysisStepStatus{
		Options: []agenticv1alpha1.RemediationOption{
			{
				Title:    "Option 1",
				Diagnosis: agenticv1alpha1.DiagnosisResult{Summary: "test", Confidence: agenticv1alpha1.ConfidenceLevelHigh, RootCause: "test"},
				Proposal:  agenticv1alpha1.ProposalResult{Description: "test", Risk: agenticv1alpha1.RiskLevelLow, Actions: []agenticv1alpha1.ProposedAction{{Type: "patch", Description: "test"}}},
			},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).WithStatusSubresource(p).Build()

	o := &ApproveOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		option:    0,
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "approved") {
		t.Errorf("expected 'approved' in output, got: %s", out.String())
	}

	// Verify the status was patched
	var updated agenticv1alpha1.Proposal
	if err := fc.Get(context.Background(), types.NamespacedName{Name: "fix-crash", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status.Phase != agenticv1alpha1.ProposalPhaseApproved {
		t.Errorf("expected Approved phase, got %s", updated.Status.Phase)
	}
	if updated.Status.Steps.Analysis.SelectedOption == nil {
		t.Fatal("expected SelectedOption to be set")
	}
	if *updated.Status.Steps.Analysis.SelectedOption != 0 {
		t.Errorf("expected SelectedOption=0, got %d", *updated.Status.Steps.Analysis.SelectedOption)
	}
}

func TestApprove_CustomOption(t *testing.T) {
	streams, _, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseProposed)
	p.Status.Steps.Analysis = agenticv1alpha1.AnalysisStepStatus{
		Options: []agenticv1alpha1.RemediationOption{
			{Title: "Option 1", Diagnosis: agenticv1alpha1.DiagnosisResult{Summary: "a", Confidence: agenticv1alpha1.ConfidenceLevelHigh, RootCause: "a"}, Proposal: agenticv1alpha1.ProposalResult{Description: "a", Risk: agenticv1alpha1.RiskLevelLow, Actions: []agenticv1alpha1.ProposedAction{{Type: "patch", Description: "a"}}}},
			{Title: "Option 2", Diagnosis: agenticv1alpha1.DiagnosisResult{Summary: "b", Confidence: agenticv1alpha1.ConfidenceLevelMedium, RootCause: "b"}, Proposal: agenticv1alpha1.ProposalResult{Description: "b", Risk: agenticv1alpha1.RiskLevelMedium, Actions: []agenticv1alpha1.ProposedAction{{Type: "scale", Description: "b"}}}},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).WithStatusSubresource(p).Build()

	o := &ApproveOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		option:    1,
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var updated agenticv1alpha1.Proposal
	if err := fc.Get(context.Background(), types.NamespacedName{Name: "fix-crash", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status.Steps.Analysis.SelectedOption == nil || *updated.Status.Steps.Analysis.SelectedOption != 1 {
		t.Errorf("expected SelectedOption=1, got %v", updated.Status.Steps.Analysis.SelectedOption)
	}
}

func TestApprove_WrongPhase(t *testing.T) {
	phases := []agenticv1alpha1.ProposalPhase{
		agenticv1alpha1.ProposalPhasePending,
		agenticv1alpha1.ProposalPhaseAnalyzing,
		agenticv1alpha1.ProposalPhaseApproved,
		agenticv1alpha1.ProposalPhaseExecuting,
		agenticv1alpha1.ProposalPhaseCompleted,
		agenticv1alpha1.ProposalPhaseFailed,
		agenticv1alpha1.ProposalPhaseDenied,
	}
	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			streams, _, _ := fakeStreams()
			p := testProposalWithStatus("fix-crash", "default", phase)

			fc := fake.NewClientBuilder().WithScheme(testScheme()).
				WithObjects(p).WithStatusSubresource(p).Build()

			o := &ApproveOptions{
				client:    fc,
				name:      "fix-crash",
				namespace: "default",
				IOStreams:  streams,
			}
			err := o.Run(context.Background())
			if err == nil {
				t.Fatal("expected error for non-Proposed phase")
			}
			if !strings.Contains(err.Error(), "must be Proposed") {
				t.Errorf("error should mention 'must be Proposed', got: %v", err)
			}
		})
	}
}

func TestApprove_OptionOutOfRange(t *testing.T) {
	streams, _, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseProposed)
	p.Status.Steps.Analysis = agenticv1alpha1.AnalysisStepStatus{
		Conditions: []metav1.Condition{
			{Type: "Analyzed", Status: metav1.ConditionTrue, Reason: "Success", LastTransitionTime: metav1.Now()},
		},
		Options: []agenticv1alpha1.RemediationOption{
			{Title: "Only option", Diagnosis: agenticv1alpha1.DiagnosisResult{Summary: "a", Confidence: agenticv1alpha1.ConfidenceLevelHigh, RootCause: "a"}, Proposal: agenticv1alpha1.ProposalResult{Description: "a", Risk: agenticv1alpha1.RiskLevelLow, Actions: []agenticv1alpha1.ProposedAction{{Type: "patch", Description: "a"}}}},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).WithStatusSubresource(p).Build()

	o := &ApproveOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		option:    5,
		IOStreams:  streams,
	}
	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for out-of-range option")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error should mention 'out of range', got: %v", err)
	}
}

func TestApprove_NotFound(t *testing.T) {
	streams, _, _ := fakeStreams()
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &ApproveOptions{
		client:    fc,
		name:      "nonexistent",
		namespace: "default",
		IOStreams:  streams,
	}
	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for nonexistent proposal")
	}
}

func TestApprove_Validate(t *testing.T) {
	o := &ApproveOptions{option: -1}
	if err := o.Validate(); err == nil {
		t.Error("expected error for negative option")
	}

	o = &ApproveOptions{option: 0}
	if err := o.Validate(); err != nil {
		t.Errorf("unexpected error for valid option: %v", err)
	}
}
