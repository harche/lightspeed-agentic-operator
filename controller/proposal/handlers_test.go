package proposal

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

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

func TestReconcile_AnalysisFailure_RetryAndEscalate(t *testing.T) {
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	agent := newTestAgentCaller()
	agent.analyzeErr = fmt.Errorf("LLM timeout")
	scheme := testScheme()

	maxAttempts := int32(2)
	proposal := testProposal("remediation")
	proposal.Spec.MaxAttempts = &maxAttempts

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: agent}

	// Reconcile 1: Pending → Failed
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

	// Reconcile 2: Failed → Pending (retry, attempt 2)
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhasePending {
		t.Fatalf("expected Pending (retry), got %s", p.Status.Phase)
	}
	if *p.Status.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", *p.Status.Attempt)
	}
	if len(p.Status.PreviousAttempts) != 1 {
		t.Fatalf("expected 1 previous attempt, got %d", len(p.Status.PreviousAttempts))
	}

	// Reconcile 3: Pending → Failed (attempt 2)
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed, got %s", p.Status.Phase)
	}

	// Reconcile 4: Failed → Escalated
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseEscalated {
		t.Fatalf("expected Escalated, got %s", p.Status.Phase)
	}
	if len(p.Status.PreviousAttempts) != 2 {
		t.Fatalf("expected 2 previous attempts, got %d", len(p.Status.PreviousAttempts))
	}

	// Reconcile 5: Escalated → creates child proposal
	reconcileOnce(r, "fix-crash")
	var child agenticv1alpha1.Proposal
	if err := fc.Get(context.Background(), types.NamespacedName{Name: "fix-crash-escalation", Namespace: "default"}, &child); err != nil {
		t.Fatalf("child proposal not found: %v", err)
	}
	if child.Spec.ParentRef == nil || child.Spec.ParentRef.Name != "fix-crash" {
		t.Error("child parentRef not set correctly")
	}

	// Verify escalation request content persisted to postgres
	escalationReq, err := store.GetRequestContent(context.Background(), "fix-crash-escalation-request")
	if err != nil {
		t.Fatalf("escalation request not in store: %v", err)
	}
	if escalationReq.Content == "" {
		t.Fatal("escalation request content is empty")
	}
}

func TestReconcile_RetryIdempotent(t *testing.T) {
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

	// Pending → Failed, then Failed → Pending (retry)
	reconcileOnce(r, "fix-crash")
	reconcileOnce(r, "fix-crash")

	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhasePending {
		t.Fatalf("expected Pending after retry, got %s", p.Status.Phase)
	}
	if *p.Status.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", *p.Status.Attempt)
	}
	if len(p.Status.PreviousAttempts) != 1 {
		t.Fatal("expected exactly 1 previous attempt")
	}
}

func TestReconcile_VerificationFailure_Retries(t *testing.T) {
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

	// Analysis → approve → execution
	reconcileOnce(r, "fix-crash")
	approveProposal(t, fc, "fix-crash")
	reconcileOnce(r, "fix-crash")

	// Make verification fail
	failed := false
	agent.verifyResult = &agenticv1alpha1.VerificationResultSpec{
		Checks:  []agenticv1alpha1.VerifyCheck{{Name: "pod-running", Source: "oc", Value: "CrashLoopBackOff", Passed: &failed}},
		Summary: "Pod still crashing",
	}

	// Verification → Failed
	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("verification reconcile: %v", err)
	}
	if !result.Requeue {
		t.Error("should requeue from Failed")
	}
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseFailed {
		t.Fatalf("expected Failed, got %s", p.Status.Phase)
	}

	// Failed → retry → Pending
	reconcileOnce(r, "fix-crash")
	p, _ = getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhasePending {
		t.Fatalf("expected Pending after retry, got %s", p.Status.Phase)
	}
	if *p.Status.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", *p.Status.Attempt)
	}
}
