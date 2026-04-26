package proposal

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

func reviseProposal(t *testing.T, fc client.WithWatch, store ContentStore, name string, revision int32, feedback string) {
	t.Helper()
	feedbackName := fmt.Sprintf("%s-revision-%d", name, revision)
	if err := store.CreateRequestContent(context.Background(), feedbackName, agenticv1alpha1.RequestContentSpec{
		ContentPayload: agenticv1alpha1.ContentPayload{Content: feedback},
	}); err != nil {
		t.Fatalf("seed revision feedback %q: %v", feedbackName, err)
	}
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

func TestReconcile_WorkflowVariants(t *testing.T) {
	tests := []struct {
		name      string
		workflow  *agenticv1alpha1.Workflow
		objects   []client.Object
		wantPhase agenticv1alpha1.ProposalPhase
	}{
		{
			name:     "full_lifecycle_reaches_verifying",
			workflow: fullWorkflow(),
			objects: []client.Object{
				testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
				testLLM("smart"), testLLM("fast"),
			},
			wantPhase: agenticv1alpha1.ProposalPhaseVerifying,
		},
		{
			name: "advisory_only_completes",
			workflow: &agenticv1alpha1.Workflow{
				ObjectMeta: metav1.ObjectMeta{Name: "advisory", Namespace: "default"},
				Spec:       agenticv1alpha1.WorkflowSpec{Analysis: corev1.LocalObjectReference{Name: "analyzer"}},
			},
			objects:   []client.Object{testAnalyzerAgent(), testLLM("smart")},
			wantPhase: agenticv1alpha1.ProposalPhaseCompleted,
		},
		{
			name: "gitops_awaits_sync",
			workflow: &agenticv1alpha1.Workflow{
				ObjectMeta: metav1.ObjectMeta{Name: "gitops", Namespace: "default"},
				Spec: agenticv1alpha1.WorkflowSpec{
					Analysis:     corev1.LocalObjectReference{Name: "analyzer"},
					Verification: &corev1.LocalObjectReference{Name: "verifier"},
				},
			},
			objects:   []client.Object{testAnalyzerAgent(), testVerifierAgent(), testLLM("smart")},
			wantPhase: agenticv1alpha1.ProposalPhaseAwaitingSync,
		},
		{
			name: "trust_mode_skips_verification",
			workflow: &agenticv1alpha1.Workflow{
				ObjectMeta: metav1.ObjectMeta{Name: "trust", Namespace: "default"},
				Spec: agenticv1alpha1.WorkflowSpec{
					Analysis:  corev1.LocalObjectReference{Name: "analyzer"},
					Execution: &corev1.LocalObjectReference{Name: "executor"},
				},
			},
			objects: []client.Object{
				testAnalyzerAgent(), testExecutorAgent(),
				testLLM("smart"), testLLM("fast"),
			},
			wantPhase: agenticv1alpha1.ProposalPhaseCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

			scheme := testScheme()
			proposal := testProposal(tt.workflow.Name)

			objs := append([]client.Object{proposal, tt.workflow}, tt.objects...)
			fc := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(objs...).
				WithStatusSubresource(proposal).Build()

			r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: newTestAgentCaller()}

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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	scheme := testScheme()
	proposal := testProposal("remediation")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: newTestAgentCaller()}

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
	if p.Status.Steps.Analysis == nil || p.Status.Steps.Analysis.Result == nil {
		t.Fatal("analysis result ref not set")
	}

	// Verify analysis result persisted to postgres
	analysisResult, err := store.GetAnalysisResult(context.Background(), p.Status.Steps.Analysis.Result.Name)
	if err != nil {
		t.Fatalf("analysis result not in store: %v", err)
	}
	if len(analysisResult.Options) == 0 {
		t.Fatal("analysis result has no options")
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

	// Verify execution result persisted to postgres
	if p.Status.Steps.Execution == nil || p.Status.Steps.Execution.Result == nil {
		t.Fatal("execution result ref not set")
	}
	execResult, err := store.GetExecutionResult(context.Background(), p.Status.Steps.Execution.Result.Name)
	if err != nil {
		t.Fatalf("execution result not in store: %v", err)
	}
	if len(execResult.ActionsTaken) == 0 {
		t.Fatal("execution result has no actions")
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

	// Verify verification result persisted to postgres
	if p.Status.Steps.Verification == nil || p.Status.Steps.Verification.Result == nil {
		t.Fatal("verification result ref not set")
	}
	verifyResult, err := store.GetVerificationResult(context.Background(), p.Status.Steps.Verification.Result.Name)
	if err != nil {
		t.Fatalf("verification result not in store: %v", err)
	}
	if verifyResult.Summary == "" {
		t.Fatal("verification result has no summary")
	}
}

func TestReconcile_AnalysisSystemFailure_Terminal(t *testing.T) {
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	agent := newTestAgentCaller()
	agent.analyzeErr = fmt.Errorf("LLM timeout")
	scheme := testScheme()

	proposal := testProposal("remediation")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: agent}

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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	agent := newTestAgentCaller()
	scheme := testScheme()

	maxAttempts := int32(2)
	proposal := testProposal("remediation")
	proposal.Spec.MaxAttempts = &maxAttempts

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: agent}

	// Analysis → approve → execution → verifying
	reconcileOnce(r, "fix-crash")
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash")

	// Make verification fail (objective failure, not system error)
	failed := false
	agent.verifyResult = &agenticv1alpha1.VerificationResultSpec{
		Checks:  []agenticv1alpha1.VerifyCheck{{Name: "pod-running", Source: "oc", Value: "CrashLoopBackOff", Passed: &failed}},
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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	agent := newTestAgentCaller()
	scheme := testScheme()

	proposal := testProposal("remediation")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: agent}

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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	agent := newTestAgentCaller()
	scheme := testScheme()

	proposal := testProposal("remediation")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: agent}

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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	agent := newTestAgentCaller()
	scheme := testScheme()

	maxAttempts := int32(1)
	proposal := testProposal("remediation")
	proposal.Spec.MaxAttempts = &maxAttempts

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: agent}

	// Full lifecycle to verification failure, retries exhausted → Proposed
	reconcileOnce(r, "fix-crash")
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash")

	failed := false
	agent.verifyResult = &agenticv1alpha1.VerificationResultSpec{
		Checks:  []agenticv1alpha1.VerifyCheck{{Name: "pod-running", Source: "oc", Value: "CrashLoopBackOff", Passed: &failed}},
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
	passed := true
	agent.verifyResult = &agenticv1alpha1.VerificationResultSpec{
		Checks:  []agenticv1alpha1.VerifyCheck{{Name: "pod-running", Source: "oc", Value: "Running", Passed: &passed}},
		Summary: "Pod running",
	}
	reviseProposal(t, fc, store, "fix-crash", 1, "Try a different approach")
	reconcileOnce(r, "fix-crash") // revision re-analysis

	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed after revision, got %s", p.Status.Phase)
	}

	// Approve and complete
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash") // execution + verification
	p, _ = getProposal(r, "fix-crash")
	// Should reach Verifying (full workflow) then complete on next reconcile
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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	scheme := testScheme()
	proposal := testProposal("remediation")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: newTestAgentCaller()}

	// Reconcile 1: Pending → Proposed
	if _, err := reconcileOnce(r, "fix-crash"); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}
	initialResult := p.Status.Steps.Analysis.Result.Name

	// Submit revision
	reviseProposal(t, fc, store, "fix-crash", 1, "Increase memory to 1024MB instead of 768MB")

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
	revisedResult := p.Status.Steps.Analysis.Result.Name
	if revisedResult == initialResult {
		t.Fatal("revision should produce a new result name")
	}
	if revisedResult != "fix-crash-analysis-1-rev1" {
		t.Fatalf("unexpected result name: %s", revisedResult)
	}

	// Verify both results exist in store
	if _, err := store.GetAnalysisResult(context.Background(), initialResult); err != nil {
		t.Fatalf("initial result missing from store: %v", err)
	}
	if _, err := store.GetAnalysisResult(context.Background(), revisedResult); err != nil {
		t.Fatalf("revised result missing from store: %v", err)
	}

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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	scheme := testScheme()
	proposal := testProposal("remediation")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: newTestAgentCaller()}

	// Initial analysis
	reconcileOnce(r, "fix-crash")

	// Revision 1
	reviseProposal(t, fc, store, "fix-crash", 1, "Try 1024MB")
	reconcileOnce(r, "fix-crash")

	// Revision 2
	reviseProposal(t, fc, store, "fix-crash", 2, "Actually, 896MB")
	reconcileOnce(r, "fix-crash")

	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}
	if *p.Status.Steps.Analysis.ObservedRevision != 2 {
		t.Fatalf("expected observedRevision 2, got %d", *p.Status.Steps.Analysis.ObservedRevision)
	}
	if p.Status.Steps.Analysis.Result.Name != "fix-crash-analysis-1-rev2" {
		t.Fatalf("unexpected result name: %s", p.Status.Steps.Analysis.Result.Name)
	}

	// All three results should exist
	for _, name := range []string{"fix-crash-analysis-1", "fix-crash-analysis-1-rev1", "fix-crash-analysis-1-rev2"} {
		if _, err := store.GetAnalysisResult(context.Background(), name); err != nil {
			t.Fatalf("result %q missing: %v", name, err)
		}
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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	scheme := testScheme()
	proposal := testProposal("remediation")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: newTestAgentCaller()}

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

	resultBefore, _ := getProposal(r, "fix-crash")
	resultNameBefore := resultBefore.Status.Steps.Analysis.Result.Name

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
	if p.Status.Steps.Analysis.Result.Name != resultNameBefore {
		t.Fatal("result should not change when revision already observed")
	}
}

func TestReconcile_RevisionResetsSelectedOption(t *testing.T) {
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	scheme := testScheme()
	proposal := testProposal("remediation")
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: newTestAgentCaller()}

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
	reviseProposal(t, fc, store, "fix-crash", 1, "Different approach please")

	// Reconcile revision
	reconcileOnce(r, "fix-crash")

	p, _ = getProposal(r, "fix-crash")
	if p.Status.Steps.Analysis.SelectedOption != nil {
		t.Fatal("selectedOption should be cleared after revision")
	}
}

func TestReconcile_RevisionAnalysisFailure(t *testing.T) {
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	agent := newTestAgentCaller()
	scheme := testScheme()
	proposal := testProposal("remediation")

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: agent}

	// Initial analysis succeeds
	reconcileOnce(r, "fix-crash")
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}

	// Submit revision, but agent will fail
	reviseProposal(t, fc, store, "fix-crash", 1, "Increase memory")
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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	agent := newTestAgentCaller()
	// Inject RBAC into analysis result so ensureExecutionRBAC is exercised
	reversible := true
	agent.analyzeResult = &agenticv1alpha1.AnalysisResultSpec{
		Options: []agenticv1alpha1.RemediationOption{{
			Title: "Increase memory",
			Diagnosis: agenticv1alpha1.DiagnosisResult{
				Summary: "OOM", Confidence: "High", RootCause: "Low limit",
			},
			Proposal: agenticv1alpha1.ProposalResult{
				Description: "Increase to 512Mi",
				Actions:     []agenticv1alpha1.ProposedAction{{Type: "patch", Description: "Patch deploy"}},
				Risk:        "Low",
				Reversible:  &reversible,
			},
			RBAC: &agenticv1alpha1.RBACResult{
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

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: agent}

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
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	agent := newTestAgentCaller()
	reversible := true
	agent.analyzeResult = &agenticv1alpha1.AnalysisResultSpec{
		Options: []agenticv1alpha1.RemediationOption{{
			Title: "Fix it",
			Diagnosis: agenticv1alpha1.DiagnosisResult{
				Summary: "Broken", Confidence: "High", RootCause: "Bug",
			},
			Proposal: agenticv1alpha1.ProposalResult{
				Description: "Apply fix",
				Actions:     []agenticv1alpha1.ProposedAction{{Type: "patch", Description: "Patch"}},
				Risk:        "Low",
				Reversible:  &reversible,
			},
			RBAC: &agenticv1alpha1.RBACResult{
				NamespaceScoped: []agenticv1alpha1.RBACRule{{
					APIGroups: []string{"apps"}, Resources: []string{"deployments"},
					Verbs: []string{"get", "patch"}, Justification: "Fix deploy",
				}},
			},
		}},
	}

	scheme := testScheme()
	proposal := testProposal("remediation")

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: agent}

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
	var binding rbacv1.RoleBinding
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "production"}, &binding); err == nil {
		t.Fatal("RoleBinding should be cleaned up after failure")
	}
}
