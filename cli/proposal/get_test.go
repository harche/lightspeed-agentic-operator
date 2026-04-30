package proposal

import (
	"context"
	"strings"
	"testing"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGet_BasicDetail(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseProposed)

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	for _, want := range []string{"Name:", "fix-crash", "Namespace:", "default", "Phase:", "Proposed"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output, got:\n%s", want, output)
		}
	}
}

func TestGet_WithAnalysisOptions(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseProposed)
	selected := int32(0)
	p.Status.Steps.Analysis = agenticv1alpha1.AnalysisStepStatus{
		Conditions: []metav1.Condition{
			{Type: "Analyzed", Status: metav1.ConditionTrue, Reason: "Success", LastTransitionTime: metav1.Now()},
		},
		Options: []agenticv1alpha1.RemediationOption{
			{
				Title: "Increase memory",
				Diagnosis: agenticv1alpha1.DiagnosisResult{
					Summary:    "OOMKilled due to low memory",
					Confidence: agenticv1alpha1.ConfidenceLevelHigh,
					RootCause:  "Memory limit 256Mi",
				},
				Proposal: agenticv1alpha1.ProposalResult{
					Description: "Increase memory to 512Mi",
					Risk:        agenticv1alpha1.RiskLevelLow,
					Actions: []agenticv1alpha1.ProposedAction{
						{Type: "patch", Description: "Patch deployment"},
					},
				},
			},
		},
		SelectedOption: &selected,
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Increase memory") {
		t.Error("expected option title in output")
	}
	if !strings.Contains(output, "[SELECTED]") {
		t.Error("expected [SELECTED] marker")
	}
	if !strings.Contains(output, "OOMKilled") {
		t.Error("expected diagnosis summary")
	}
	if !strings.Contains(output, "risk=Low") {
		t.Error("expected risk level")
	}
}

func TestGet_WithExecutionActions(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseExecuting)
	p.Status.Steps.Execution = agenticv1alpha1.ExecutionStepStatus{
		Conditions: []metav1.Condition{
			{Type: "Executed", Status: metav1.ConditionUnknown, Reason: "InProgress", LastTransitionTime: metav1.Now()},
		},
		ActionsTaken: []agenticv1alpha1.ExecutionAction{
			{Type: "patch", Description: "Increased memory to 512Mi", Outcome: agenticv1alpha1.ActionOutcomeSucceeded},
			{Type: "restart", Description: "Rolled out deployment", Outcome: agenticv1alpha1.ActionOutcomeFailed},
		},
		Verification: agenticv1alpha1.ExecutionVerification{
			ConditionOutcome: agenticv1alpha1.ConditionOutcomeImproved,
			Summary:          "Pod is running",
		},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "outcome=Succeeded") {
		t.Error("expected Succeeded outcome")
	}
	if !strings.Contains(output, "outcome=Failed") {
		t.Error("expected Failed outcome")
	}
	if !strings.Contains(output, "Inline Verify") {
		t.Error("expected inline verification section")
	}
	if !strings.Contains(output, "condition=Improved") {
		t.Error("expected condition outcome")
	}
}

func TestGet_WithVerificationChecks(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseVerifying)
	p.Status.Steps.Verification = agenticv1alpha1.VerificationStepStatus{
		Conditions: []metav1.Condition{
			{Type: "Verified", Status: metav1.ConditionTrue, Reason: "AllPassed", LastTransitionTime: metav1.Now()},
		},
		Checks: []agenticv1alpha1.VerifyCheck{
			{Name: "pod-running", Source: "oc", Value: "Running", Result: agenticv1alpha1.CheckResultPassed},
			{Name: "memory-ok", Source: "oc", Value: "512Mi", Result: agenticv1alpha1.CheckResultPassed},
		},
		Summary: "All checks passed",
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "pod-running") {
		t.Error("expected check name in output")
	}
	if !strings.Contains(output, "Passed") {
		t.Error("expected Passed result")
	}
	if !strings.Contains(output, "All checks passed") {
		t.Error("expected summary")
	}
}

func TestGet_WithPreviousAttempts(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseAnalyzing)
	attempt := int32(2)
	p.Status.Attempts = &attempt
	p.Status.PreviousAttempts = []agenticv1alpha1.PreviousAttempt{
		{Attempt: 1, FailedStep: agenticv1alpha1.SandboxStepExecution, FailureReason: "Timeout after 5m"},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Previous Attempts:") {
		t.Error("expected previous attempts section")
	}
	if !strings.Contains(output, "Execution") {
		t.Error("expected failed step")
	}
	if !strings.Contains(output, "Timeout after 5m") {
		t.Error("expected failure reason")
	}
}

func TestGet_WithParentRef(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("child-fix", "default", agenticv1alpha1.ProposalPhasePending)
	p.Spec.Parent = agenticv1alpha1.ProposalReference{Name: "parent-fix"}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "child-fix",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "Parent:") || !strings.Contains(out.String(), "parent-fix") {
		t.Error("expected parent ref in output")
	}
}

func TestGet_WithConditions(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseCompleted)
	p.Status.Conditions = []metav1.Condition{
		{Type: "Analyzed", Status: metav1.ConditionTrue, Reason: "Success", LastTransitionTime: metav1.Now()},
		{Type: "Approved", Status: metav1.ConditionTrue, Reason: "UserApproved", LastTransitionTime: metav1.Now()},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Conditions:") {
		t.Error("expected conditions section")
	}
	if !strings.Contains(output, "Analyzed") {
		t.Error("expected Analyzed condition")
	}
}

func TestGet_JSONOutput(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseProposed)

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		output:    "json",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), `"request"`) || !strings.Contains(out.String(), `"fix-crash"`) {
		t.Errorf("expected JSON output with proposal fields, got:\n%s", out.String())
	}
}

func TestGet_NotFound(t *testing.T) {
	streams, _, _ := fakeStreams()
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &GetOptions{
		client:    fc,
		name:      "nonexistent",
		namespace: "default",
		IOStreams:  streams,
	}
	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for nonexistent proposal")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention proposal name, got: %v", err)
	}
}

func TestGet_StepStatusFromConditions(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseAnalyzing)
	p.Status.Steps.Analysis = agenticv1alpha1.AnalysisStepStatus{
		Conditions: []metav1.Condition{
			{Type: "Analyzed", Status: metav1.ConditionUnknown, Reason: "InProgress", LastTransitionTime: metav1.Now()},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Unknown") {
		t.Error("expected Unknown status for in-progress analysis")
	}
	if !strings.Contains(output, "InProgress") {
		t.Error("expected InProgress reason")
	}
}

func TestGet_NoStepConditions(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhasePending)

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).Build()

	o := &GetOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	// When no conditions, step status should show "-"
	if !strings.Contains(output, "Analysis:          -") {
		t.Errorf("expected '-' for analysis with no conditions, got:\n%s", output)
	}
}
