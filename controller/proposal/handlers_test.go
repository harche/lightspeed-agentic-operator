package proposal

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

func reviseProposal(t *testing.T, fc client.WithWatch, name string, revision int32) {
	t.Helper()
	var p agenticv1alpha1.Proposal
	if err := fc.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &p); err != nil {
		t.Fatalf("get proposal for revision: %v", err)
	}
	original := p.DeepCopy()
	p.Spec.Revision = &revision
	if err := fc.Patch(context.Background(), &p, client.MergeFrom(original)); err != nil {
		t.Fatalf("patch revision: %v", err)
	}
}

func TestReconcile_TemplateVariants(t *testing.T) {
	tests := []struct {
		name      string
		template  *agenticv1alpha1.ProposalTemplate
		wantPhase agenticv1alpha1.ProposalPhase
	}{
		{
			name:      "full_lifecycle_reaches_verifying",
			template:  fullTemplate(),
			wantPhase: agenticv1alpha1.ProposalPhaseVerifying,
		},
		{
			name: "advisory_only_completes",
			template: &agenticv1alpha1.ProposalTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "advisory"},
				Spec: agenticv1alpha1.ProposalTemplateSpec{
					Analysis: agenticv1alpha1.TemplateStep{Agent: "default"},
				},
			},
			wantPhase: agenticv1alpha1.ProposalPhaseCompleted,
		},
		{
			name: "assisted_awaits_sync",
			template: &agenticv1alpha1.ProposalTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "assisted"},
				Spec: agenticv1alpha1.ProposalTemplateSpec{
					Analysis:     agenticv1alpha1.TemplateStep{Agent: "default"},
					Verification: &agenticv1alpha1.TemplateStep{Agent: "default"},
				},
			},
			wantPhase: agenticv1alpha1.ProposalPhaseAwaitingSync,
		},
		{
			name: "no_verification_skips_verification",
			template: &agenticv1alpha1.ProposalTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "no-verify"},
				Spec: agenticv1alpha1.ProposalTemplateSpec{
					Analysis:  agenticv1alpha1.TemplateStep{Agent: "default"},
					Execution: &agenticv1alpha1.TemplateStep{Agent: "default"},
				},
			},
			wantPhase: agenticv1alpha1.ProposalPhaseCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := testScheme()
			proposal := testProposal(tt.template.Name)

			objs := append([]client.Object{proposal, tt.template}, testDefaultAgent(), testLLM("smart"))
			fc := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(objs...).
				WithStatusSubresource(proposal).Build()

			r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: newTestAgentCaller()}

			if _, err := reconcileOnce(r, "fix-crash"); err != nil {
				t.Fatalf("analysis reconcile: %v", err)
			}
			p, _ := getProposal(r, "fix-crash")
			if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
				t.Fatalf("after analysis: expected Proposed, got %s", p.Status.Phase)
			}

			approveProposal(t, fc, "fix-crash")

			if _, err := reconcileOnce(r, "fix-crash"); err != nil {
				t.Fatalf("post-approval reconcile: %v", err)
			}
			p, _ = getProposal(r, "fix-crash")
			if p.Status.Phase != tt.wantPhase {
				t.Fatalf("after approval: expected %s, got %s", tt.wantPhase, p.Status.Phase)
			}
		})
	}
}

func TestReconcile_HappyPath_FullLifecycle(t *testing.T) {
	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: newTestAgentCaller()}

	// Reconcile 1: Pending → Proposed
	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if result.Requeue {
		t.Error("should not requeue after analysis")
	}

	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}
	if len(p.Status.Steps.Analysis.Options) == 0 {
		t.Fatal("analysis options not set")
	}

	// Approve
	approveProposal(t, fc, "fix-crash")

	// Reconcile 2: Approved → Verifying
	result, err = reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if !result.Requeue {
		t.Error("should requeue to enter verification")
	}

	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying, got %s", p.Status.Phase)
	}
	if len(p.Status.Steps.Execution.ActionsTaken) == 0 {
		t.Fatal("execution actions not set")
	}

	// Reconcile 3: Verifying → Completed
	_, err = reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("reconcile 3: %v", err)
	}

	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseCompleted {
		t.Fatalf("expected Completed, got %s", p.Status.Phase)
	}
	if p.Status.Steps.Verification.Summary == "" {
		t.Fatal("verification summary not set")
	}
}

func TestReconcile_AnalysisSystemFailure_Terminal(t *testing.T) {
	agent := newTestAgentCaller()
	agent.analyzeErr = fmt.Errorf("LLM timeout")
	scheme := testScheme()

	proposal := testProposal("remediation")
	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Reconcile 1: Pending → Failed (system failure)
	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if !result.Requeue {
		t.Error("should requeue to enter handleFailed")
	}
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed, got %s", p.Status.Phase)
	}

	// Reconcile 2: Failed stays Failed (terminal, no retry)
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed (terminal), got %s", p.Status.Phase)
	}
	if len(p.Status.PreviousAttempts) != 1 {
		t.Fatalf("expected 1 previous attempt recorded, got %d", len(p.Status.PreviousAttempts))
	}
}

func TestReconcile_VerificationObjectiveFailure_RetriesExecution(t *testing.T) {
	agent := newTestAgentCaller()
	scheme := testScheme()

	maxAttempts := int32(2)
	proposal := testProposal("remediation")
	proposal.Spec.MaxAttempts = &maxAttempts

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Analysis → approve → execution → verifying
	reconcileOnce(r, "fix-crash")
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash")

	// Make verification fail (objective failure, not system error)
	agent.verifyResult = &VerificationOutput{
		Success: false,
		Checks:  []agenticv1alpha1.VerifyCheck{{Name: "pod-running", Source: "oc", Value: "CrashLoopBackOff", Result: agenticv1alpha1.CheckResultFailed}},
		Summary: "Pod still crashing",
	}

	// Verification fails → back to Approved for retry (retryCount=1)
	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("verification reconcile: %v", err)
	}
	if !result.Requeue {
		t.Error("should requeue to re-execute")
	}
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseApproved {
		t.Fatalf("expected Approved (retry), got %s", p.Status.Phase)
	}
	if p.Status.Steps.Execution.RetryCount == nil || *p.Status.Steps.Execution.RetryCount != 1 {
		t.Fatal("retryCount should be 1")
	}

	// Re-execute → Verifying
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying (re-execution), got %s", p.Status.Phase)
	}

	// Re-verify → fails again → Approved (retryCount=2, requeue)
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseApproved {
		t.Fatalf("expected Approved (retry 2), got %s", p.Status.Phase)
	}
	if *p.Status.Steps.Execution.RetryCount != 2 {
		t.Fatalf("expected retryCount 2, got %d", *p.Status.Steps.Execution.RetryCount)
	}

	// Re-execute again → Verifying
	reconcileOnce(r, "fix-crash")
	// Re-verify → retryCount=2 >= maxAttempts=2 → Proposed (exhausted)
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed (retries exhausted), got %s", p.Status.Phase)
	}
	if p.Status.Steps.Analysis.SelectedOption != nil {
		t.Fatal("selectedOption should be cleared after retries exhausted")
	}
}

func TestReconcile_SystemFailure_Execution_Terminal(t *testing.T) {
	agent := newTestAgentCaller()
	scheme := testScheme()

	proposal := testProposal("remediation")
	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Analysis → approve
	reconcileOnce(r, "fix-crash")
	approveProposal(t, fc, "fix-crash")

	// Execution system failure
	agent.executeErr = fmt.Errorf("sandbox pod crashed")
	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !result.Requeue {
		t.Error("should requeue to enter handleFailed")
	}
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed, got %s", p.Status.Phase)
	}

	// Terminal — stays Failed
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed (terminal), got %s", p.Status.Phase)
	}
}

func TestReconcile_SystemFailure_Verification_Terminal(t *testing.T) {
	agent := newTestAgentCaller()
	scheme := testScheme()

	proposal := testProposal("remediation")
	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Analysis → approve → execution → verifying
	reconcileOnce(r, "fix-crash")
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash")

	// Verification system failure
	agent.verifyErr = fmt.Errorf("network unreachable")
	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !result.Requeue {
		t.Error("should requeue to enter handleFailed")
	}
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed, got %s", p.Status.Phase)
	}

	// Terminal — stays Failed
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed (terminal), got %s", p.Status.Phase)
	}
}

func TestReconcile_ObjectiveFailure_ThenRevise(t *testing.T) {
	agent := newTestAgentCaller()
	scheme := testScheme()

	maxAttempts := int32(1)
	proposal := testProposal("remediation")
	proposal.Spec.MaxAttempts = &maxAttempts

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Full lifecycle to verification failure, retries exhausted → Proposed
	reconcileOnce(r, "fix-crash")
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash")

	agent.verifyResult = &VerificationOutput{
		Checks:  []agenticv1alpha1.VerifyCheck{{Name: "pod-running", Source: "oc", Value: "CrashLoopBackOff", Result: agenticv1alpha1.CheckResultFailed}},
		Summary: "Pod still crashing",
	}
	// Verification fails → Approved (retry, retryCount=1)
	reconcileOnce(r, "fix-crash")
	// Re-execute → Verifying
	reconcileOnce(r, "fix-crash")
	// Re-verify → retryCount=1 >= maxAttempts=1 → Proposed
	reconcileOnce(r, "fix-crash")

	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}

	// Admin submits revision
	agent.verifyResult = &VerificationOutput{
		Success: true,
		Checks:  []agenticv1alpha1.VerifyCheck{{Name: "pod-running", Source: "oc", Value: "Running", Result: agenticv1alpha1.CheckResultPassed}},
		Summary: "Pod running",
	}
	reviseProposal(t, fc, "fix-crash", 1)
	reconcileOnce(r, "fix-crash") // revision re-analysis

	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed after revision, got %s", p.Status.Phase)
	}

	// Approve and complete
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash") // execution + verification
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying, got %s", p.Status.Phase)
	}
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseCompleted {
		t.Fatalf("expected Completed, got %s", p.Status.Phase)
	}
}

func TestReconcile_RevisionHappyPath(t *testing.T) {
	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: newTestAgentCaller()}

	// Reconcile 1: Pending → Proposed
	if _, err := reconcileOnce(r, "fix-crash"); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}
	initialOptionsCount := len(p.Status.Steps.Analysis.Options)

	// Submit revision
	reviseProposal(t, fc, "fix-crash", 1)

	// Reconcile 2: Proposed → Analyzing → Proposed (revised)
	if _, err := reconcileOnce(r, "fix-crash"); err != nil {
		t.Fatalf("reconcile 2 (revision): %v", err)
	}
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed after revision, got %s", p.Status.Phase)
	}
	if p.Status.Steps.Analysis.ObservedRevision == nil || *p.Status.Steps.Analysis.ObservedRevision != 1 {
		t.Fatal("observedRevision not set to 1")
	}
	if len(p.Status.Steps.Analysis.Options) == 0 {
		t.Fatal("options should be populated after revision")
	}
	_ = initialOptionsCount // options are replaced inline

	// Approve and continue
	approveProposal(t, fc, "fix-crash")
	if _, err := reconcileOnce(r, "fix-crash"); err != nil {
		t.Fatalf("reconcile 3 (post-approval): %v", err)
	}
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying after approval, got %s", p.Status.Phase)
	}
}

func TestReconcile_RevisionMultipleRounds(t *testing.T) {
	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: newTestAgentCaller()}

	// Initial analysis
	reconcileOnce(r, "fix-crash")

	// Revision 1
	reviseProposal(t, fc, "fix-crash", 1)
	reconcileOnce(r, "fix-crash")

	// Revision 2
	reviseProposal(t, fc, "fix-crash", 2)
	reconcileOnce(r, "fix-crash")

	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}
	if *p.Status.Steps.Analysis.ObservedRevision != 2 {
		t.Fatalf("expected observedRevision 2, got %d", *p.Status.Steps.Analysis.ObservedRevision)
	}

	// Approve and proceed
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying, got %s", p.Status.Phase)
	}
}

func TestReconcile_RevisionNoOp_WhenObserved(t *testing.T) {
	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: newTestAgentCaller()}

	// Initial analysis
	reconcileOnce(r, "fix-crash")

	// Simulate already-observed revision
	p, _ := getProposal(r, "fix-crash")
	base := p.DeepCopy()
	rev := int32(1)
	p.Spec.Revision = &rev
	if err := fc.Patch(context.Background(), p, client.MergeFrom(base)); err != nil {
		t.Fatalf("patch spec revision: %v", err)
	}
	p, _ = getProposal(r, "fix-crash")
	base = p.DeepCopy()
	p.Status.Steps.Analysis.ObservedRevision = &rev
	if err := fc.Status().Patch(context.Background(), p, client.MergeFrom(base)); err != nil {
		t.Fatalf("patch status observedRevision: %v", err)
	}

	// Reconcile should be a no-op
	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Requeue {
		t.Error("should not requeue when revision already observed")
	}

	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}
}

func TestReconcile_RevisionResetsSelectedOption(t *testing.T) {
	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: newTestAgentCaller()}

	// Analysis → Proposed
	reconcileOnce(r, "fix-crash")

	// Set selectedOption (user started reviewing)
	p, _ := getProposal(r, "fix-crash")
	base := p.DeepCopy()
	selected := int32(0)
	p.Status.Steps.Analysis.SelectedOption = &selected
	if err := fc.Status().Patch(context.Background(), p, client.MergeFrom(base)); err != nil {
		t.Fatalf("set selectedOption: %v", err)
	}

	// Submit revision
	reviseProposal(t, fc, "fix-crash", 1)

	// Reconcile revision
	reconcileOnce(r, "fix-crash")

	p, _ = getProposal(r, "fix-crash")
	if p.Status.Steps.Analysis.SelectedOption != nil {
		t.Fatal("selectedOption should be cleared after revision")
	}
}

func TestReconcile_RevisionAnalysisFailure(t *testing.T) {
	agent := newTestAgentCaller()
	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Initial analysis succeeds
	reconcileOnce(r, "fix-crash")
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}

	// Submit revision, but agent will fail
	reviseProposal(t, fc, "fix-crash", 1)
	agent.analyzeErr = fmt.Errorf("LLM timeout during revision")

	// Reconcile → revision analysis fails → Failed
	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !result.Requeue {
		t.Error("should requeue to enter handleFailed")
	}
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed, got %s", p.Status.Phase)
	}

	// Failed is terminal for system failures — stays Failed
	agent.analyzeErr = nil
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed (terminal), got %s", p.Status.Phase)
	}
}

func TestReconcile_ExecutionRBACCreatedOnApproval(t *testing.T) {
	agent := newTestAgentCaller()
	agent.analyzeResult = &AnalysisOutput{
		Success: true,
		Options: []agenticv1alpha1.RemediationOption{{
			Title: "Increase memory",
			Diagnosis: agenticv1alpha1.DiagnosisResult{
				Summary: "OOM", Confidence: "High", RootCause: "Low limit",
			},
			Proposal: agenticv1alpha1.ProposalResult{
				Description: "Increase to 512Mi",
				Actions:     []agenticv1alpha1.ProposedAction{{Type: "patch", Description: "Patch deploy"}},
				Risk:        "Low",
				Reversible:  agenticv1alpha1.ReversibilityReversible,
			},
			RBAC: agenticv1alpha1.RBACResult{
				NamespaceScoped: []agenticv1alpha1.RBACRule{{
					APIGroups:     []string{"apps"},
					Resources:     []string{"deployments"},
					Verbs:         []string{"get", "patch"},
					Justification: "Patch deployment memory",
				}},
				ClusterScoped: []agenticv1alpha1.RBACRule{{
					APIGroups:     []string{""},
					Resources:     []string{"nodes"},
					Verbs:         []string{"get", "list"},
					Justification: "Check node capacity",
				}},
			},
		}},
	}

	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Pending → Proposed
	reconcileOnce(r, "fix-crash")
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}

	// Approve
	approveProposal(t, fc, "fix-crash")

	// Approved → Executing → Verifying
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying, got %s", p.Status.Phase)
	}

	// Verify namespace-scoped Role+RoleBinding were created
	roleName := executionRoleName("fix-crash")
	var role rbacv1.Role
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "production"}, &role); err != nil {
		t.Fatalf("execution Role not created in production: %v", err)
	}
	if role.Rules[0].Resources[0] != "deployments" {
		t.Fatalf("unexpected Role rule: %+v", role.Rules)
	}
	var binding rbacv1.RoleBinding
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "production"}, &binding); err != nil {
		t.Fatalf("execution RoleBinding not created: %v", err)
	}

	// Verify cluster-scoped ClusterRole+ClusterRoleBinding were created
	crName := clusterRoleName("fix-crash")
	var cr rbacv1.ClusterRole
	if err := fc.Get(context.Background(), types.NamespacedName{Name: crName}, &cr); err != nil {
		t.Fatalf("execution ClusterRole not created: %v", err)
	}
	if cr.Rules[0].Resources[0] != "nodes" {
		t.Fatalf("unexpected ClusterRole rule: %+v", cr.Rules)
	}
	var crb rbacv1.ClusterRoleBinding
	if err := fc.Get(context.Background(), types.NamespacedName{Name: crName}, &crb); err != nil {
		t.Fatalf("execution ClusterRoleBinding not created: %v", err)
	}

	// Verify rbac-namespaces annotation was set
	p, _ = getProposal(r, "fix-crash")
	if p.Annotations[rbacNamespacesAnnotation] != "production" {
		t.Fatalf("expected rbac-namespaces annotation 'production', got %q", p.Annotations[rbacNamespacesAnnotation])
	}

	// Complete lifecycle
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseCompleted {
		t.Fatalf("expected Completed, got %s", p.Status.Phase)
	}
}

func TestReconcile_ExecutionRBACCleanedOnFailure(t *testing.T) {
	agent := newTestAgentCaller()
	agent.analyzeResult = &AnalysisOutput{
		Success: true,
		Options: []agenticv1alpha1.RemediationOption{{
			Title: "Fix it",
			Diagnosis: agenticv1alpha1.DiagnosisResult{
				Summary: "Broken", Confidence: "High", RootCause: "Bug",
			},
			Proposal: agenticv1alpha1.ProposalResult{
				Description: "Apply fix",
				Actions:     []agenticv1alpha1.ProposedAction{{Type: "patch", Description: "Patch"}},
				Risk:        "Low",
				Reversible:  agenticv1alpha1.ReversibilityReversible,
			},
			RBAC: agenticv1alpha1.RBACResult{
				NamespaceScoped: []agenticv1alpha1.RBACRule{{
					APIGroups: []string{"apps"}, Resources: []string{"deployments"},
					Verbs: []string{"get", "patch"}, Justification: "Fix deploy",
				}},
			},
		}},
	}

	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Analysis → approve
	reconcileOnce(r, "fix-crash")
	approveProposal(t, fc, "fix-crash")

	// Execution succeeds, creates RBAC, but verification will fail with system error
	reconcileOnce(r, "fix-crash")
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying, got %s", p.Status.Phase)
	}

	// Verify RBAC exists before failure
	roleName := executionRoleName("fix-crash")
	var role rbacv1.Role
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "production"}, &role); err != nil {
		t.Fatalf("Role should exist before failure: %v", err)
	}

	// System failure during verification
	agent.verifyErr = fmt.Errorf("sandbox pod crashed")
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed, got %s", p.Status.Phase)
	}

	// handleFailed should clean up RBAC
	reconcileOnce(r, "fix-crash")

	// Verify RBAC was cleaned up
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "production"}, &role); err == nil {
		t.Fatal("Role should be cleaned up after failure")
	}
	var bindingCheck rbacv1.RoleBinding
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "production"}, &bindingCheck); err == nil {
		t.Fatal("RoleBinding should be cleaned up after failure")
	}
}

// TestFullLifecycle_WithSandboxAgent exercises the full Pending → Completed
// lifecycle using SandboxAgentCaller with mocked sandbox and HTTP, proving
// the real agent caller implementation works through the reconciler.
func TestFullLifecycle_WithSandboxAgent(t *testing.T) {
	analysisJSON, _ := json.Marshal(analysisResponse{
		Success: true,
		Options: []agenticv1alpha1.RemediationOption{{
			Title: "Increase memory limit",
			Diagnosis: agenticv1alpha1.DiagnosisResult{
				Summary:    "Pod OOMKilled due to 256Mi memory limit",
				Confidence: "High",
				RootCause:  "Memory limit too low for workload",
			},
			Proposal: agenticv1alpha1.ProposalResult{
				Description: "Increase deployment memory limit to 512Mi",
				Actions:     []agenticv1alpha1.ProposedAction{{Type: "patch", Description: "Patch deployment memory limit"}},
				Risk:        "Low",
				Reversible:  agenticv1alpha1.ReversibilityReversible,
			},
		}},
	})

	executionJSON, _ := json.Marshal(executionResponse{
		Success: true,
		ActionsTaken: []agenticv1alpha1.ExecutionAction{{
			Type:        "patch",
			Description: "Patched deployment/web memory limit to 512Mi",
			Outcome:     agenticv1alpha1.ActionOutcomeSucceeded,
		}},
		Verification: &agenticv1alpha1.ExecutionVerification{
			ConditionOutcome: agenticv1alpha1.ConditionOutcomeImproved,
			Summary:          "Pod running with new memory limit",
		},
	})

	verificationJSON, _ := json.Marshal(verificationResponse{
		Success: true,
		Checks:  []agenticv1alpha1.VerifyCheck{{
			Name:   "pod-running",
			Source: "oc",
			Value:  "Running",
			Result: agenticv1alpha1.CheckResultPassed,
		}},
		Summary: "All verification checks passed",
	})

	sandboxAgent, sandbox := newMockSandboxAgent(string(analysisJSON), string(executionJSON), string(verificationJSON))

	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: sandboxAgent}

	// Reconcile 1: Pending → Proposed (via sandbox analysis)
	if _, err := reconcileOnce(r, "fix-crash"); err != nil {
		t.Fatalf("analysis reconcile: %v", err)
	}
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}
	if len(p.Status.Steps.Analysis.Options) != 1 {
		t.Fatalf("expected 1 option, got %d", len(p.Status.Steps.Analysis.Options))
	}
	if p.Status.Steps.Analysis.Options[0].Title != "Increase memory limit" {
		t.Errorf("option title = %q", p.Status.Steps.Analysis.Options[0].Title)
	}
	if p.Status.Steps.Analysis.Options[0].Diagnosis.Confidence != "High" {
		t.Errorf("confidence = %q", p.Status.Steps.Analysis.Options[0].Diagnosis.Confidence)
	}

	// Approve
	approveProposal(t, fc, "fix-crash")

	// Reconcile 2: Approved → Executing → Verifying (via sandbox execution)
	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("execution reconcile: %v", err)
	}
	if !result.Requeue {
		t.Error("should requeue to enter verification")
	}
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying, got %s", p.Status.Phase)
	}
	if len(p.Status.Steps.Execution.ActionsTaken) != 1 {
		t.Fatalf("expected 1 action, got %d", len(p.Status.Steps.Execution.ActionsTaken))
	}
	if p.Status.Steps.Execution.ActionsTaken[0].Outcome != agenticv1alpha1.ActionOutcomeSucceeded {
		t.Errorf("action outcome = %q", p.Status.Steps.Execution.ActionsTaken[0].Outcome)
	}
	if p.Status.Steps.Execution.Verification.ConditionOutcome != agenticv1alpha1.ConditionOutcomeImproved {
		t.Errorf("inline verification = %q", p.Status.Steps.Execution.Verification.ConditionOutcome)
	}

	// Reconcile 3: Verifying → Completed (via sandbox verification)
	if _, err := reconcileOnce(r, "fix-crash"); err != nil {
		t.Fatalf("verification reconcile: %v", err)
	}
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseCompleted {
		t.Fatalf("expected Completed, got %s", p.Status.Phase)
	}
	if p.Status.Steps.Verification.Summary != "All verification checks passed" {
		t.Errorf("summary = %q", p.Status.Steps.Verification.Summary)
	}
	if len(p.Status.Steps.Verification.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(p.Status.Steps.Verification.Checks))
	}
	if p.Status.Steps.Verification.Checks[0].Result != agenticv1alpha1.CheckResultPassed {
		t.Errorf("check result = %q", p.Status.Steps.Verification.Checks[0].Result)
	}

	// Verify sandbox was claimed for each phase (release is deferred to terminal phase)
	if sandbox.claimCalls != 3 {
		t.Errorf("sandbox claim calls = %d, want 3 (analysis + execution + verification)", sandbox.claimCalls)
	}
	if sandbox.releaseCalls != 0 {
		t.Errorf("sandbox release calls = %d, want 0 (reconciler releases at terminal phase)", sandbox.releaseCalls)
	}
}

func TestReconcile_ExecutingPhase_DoesNotReExecute(t *testing.T) {
	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	agent := newTestAgentCaller()
	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Run analysis
	reconcileOnce(r, "fix-crash")

	// Approve → execution runs → phase should be Verifying
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash")

	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying after execution, got %s", p.Status.Phase)
	}

	// Simulate the bug: manually set phase back to Executing (as if status patch raced)
	base := p.DeepCopy()
	p.Status.Phase = agenticv1alpha1.ProposalPhaseExecuting
	if err := fc.Status().Patch(context.Background(), p, client.MergeFrom(base)); err != nil {
		t.Fatalf("patch phase to Executing: %v", err)
	}

	// Reconcile again — should NOT re-execute, should go to Verifying via re-entry guard
	reconcileOnce(r, "fix-crash")

	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseVerifying {
		t.Fatalf("expected Verifying from re-entry guard, got %s", p.Status.Phase)
	}
}

func TestReconcile_ExecutionSuccessFalse_FailsStep(t *testing.T) {
	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	agent := newTestAgentCaller()
	agent.executeResult = &ExecutionOutput{
		Success:      false,
		ActionsTaken: []agenticv1alpha1.ExecutionAction{{Type: "patch", Description: "Failed patch", Outcome: agenticv1alpha1.ActionOutcomeFailed}},
	}
	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Analysis → Proposed
	reconcileOnce(r, "fix-crash")
	// Approve
	approveProposal(t, fc, "fix-crash")
	// Execution with success=false → Failed
	reconcileOnce(r, "fix-crash")

	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed when execution success=false, got %s", p.Status.Phase)
	}
}

func TestReconcile_VerificationSuccessFalse_RetriesExecution(t *testing.T) {
	scheme := testScheme()
	proposal := testProposal("remediation")

	objs := append([]client.Object{proposal}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	agent := newTestAgentCaller()
	agent.verifyResult = &VerificationOutput{
		Success: false,
		Checks:  []agenticv1alpha1.VerifyCheck{{Name: "health", Result: agenticv1alpha1.CheckResultFailed}},
		Summary: "Health check failed",
	}
	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: agent}

	// Analysis → Proposed → Approve → Execute → Verify (fail) → retry
	reconcileOnce(r, "fix-crash")
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash") // execution
	reconcileOnce(r, "fix-crash") // verification → retry

	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseApproved {
		t.Fatalf("expected Approved (retry) when verification success=false, got %s", p.Status.Phase)
	}
	if p.Status.Steps.Execution.RetryCount == nil || *p.Status.Steps.Execution.RetryCount != 1 {
		t.Fatal("retryCount should be 1")
	}
}
