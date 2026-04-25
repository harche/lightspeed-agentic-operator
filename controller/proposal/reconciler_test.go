package proposal

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

// --- Configurable agent stub for tests ---

type testAgentCaller struct {
	analyzeErr error
	executeErr error
	verifyErr  error

	analyzeResult *agenticv1alpha1.AnalysisResultSpec
	executeResult *agenticv1alpha1.ExecutionResultSpec
	verifyResult  *agenticv1alpha1.VerificationResultSpec
}

func newTestAgentCaller() *testAgentCaller {
	stub := &StubAgentCaller{}
	a, _ := stub.Analyze(context.Background(), nil, resolvedStep{}, nil)
	e, _ := stub.Execute(context.Background(), nil, resolvedStep{}, nil)
	v, _ := stub.Verify(context.Background(), nil, resolvedStep{}, nil, nil)
	return &testAgentCaller{analyzeResult: a, executeResult: e, verifyResult: v}
}

func (ta *testAgentCaller) Analyze(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RequestContentSpec) (*agenticv1alpha1.AnalysisResultSpec, error) {
	if ta.analyzeErr != nil {
		return nil, ta.analyzeErr
	}
	return ta.analyzeResult, nil
}
func (ta *testAgentCaller) Execute(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RemediationOption) (*agenticv1alpha1.ExecutionResultSpec, error) {
	if ta.executeErr != nil {
		return nil, ta.executeErr
	}
	return ta.executeResult, nil
}
func (ta *testAgentCaller) Verify(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RemediationOption, _ *agenticv1alpha1.ExecutionResultSpec) (*agenticv1alpha1.VerificationResultSpec, error) {
	if ta.verifyErr != nil {
		return nil, ta.verifyErr
	}
	return ta.verifyResult, nil
}

// --- Test fixtures ---

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = agenticv1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = rbacv1.AddToScheme(s)
	return s
}

func seedRequestContent(t *testing.T, store ContentStore, name, text string) {
	t.Helper()
	if err := store.CreateRequestContent(context.Background(), name, agenticv1alpha1.RequestContentSpec{
		ContentPayload: agenticv1alpha1.ContentPayload{Content: text},
	}); err != nil {
		t.Fatalf("seed request content %q: %v", name, err)
	}
}

func fullWorkflow() *agenticv1alpha1.Workflow {
	return &agenticv1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "remediation", Namespace: "default"},
		Spec: agenticv1alpha1.WorkflowSpec{
			Analysis:     corev1.LocalObjectReference{Name: "analyzer"},
			Execution:    &corev1.LocalObjectReference{Name: "executor"},
			Verification: &corev1.LocalObjectReference{Name: "verifier"},
		},
	}
}

func testAnalyzerAgent() *agenticv1alpha1.Agent {
	return &agenticv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "analyzer", Namespace: "default"},
		Spec: agenticv1alpha1.AgentSpec{
			LLMRef: corev1.LocalObjectReference{Name: "smart"},
			Skills: []agenticv1alpha1.SkillsSource{{Image: "registry.example.com/skills:latest"}},
		},
	}
}

func testExecutorAgent() *agenticv1alpha1.Agent {
	return &agenticv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "executor", Namespace: "default"},
		Spec: agenticv1alpha1.AgentSpec{
			LLMRef: corev1.LocalObjectReference{Name: "fast"},
			Skills: []agenticv1alpha1.SkillsSource{{Image: "registry.example.com/skills:latest"}},
		},
	}
}

func testVerifierAgent() *agenticv1alpha1.Agent {
	return &agenticv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "verifier", Namespace: "default"},
		Spec: agenticv1alpha1.AgentSpec{
			LLMRef: corev1.LocalObjectReference{Name: "smart"},
			Skills: []agenticv1alpha1.SkillsSource{{Image: "registry.example.com/skills:latest"}},
		},
	}
}

func testLLM(name string) *agenticv1alpha1.LLMProvider {
	return &agenticv1alpha1.LLMProvider{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: agenticv1alpha1.LLMProviderSpec{
			Type:                 agenticv1alpha1.LLMProviderVertex,
			Model:                "claude-opus-4-6",
			CredentialsSecretRef: corev1.LocalObjectReference{Name: "llm-secret"},
		},
	}
}

func testProposal(workflowName string) *agenticv1alpha1.Proposal {
	return &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fix-crash", Namespace: "default"},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:          agenticv1alpha1.ContentReference{Name: "fix-crash-request"},
			WorkflowRef:      corev1.LocalObjectReference{Name: workflowName},
			TargetNamespaces: []string{"production"},
		},
	}
}

func reconcileOnce(r *ProposalReconciler, name string) (ctrl.Result, error) {
	return r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
	})
}

func getProposal(r *ProposalReconciler, name string) (*agenticv1alpha1.Proposal, error) {
	var p agenticv1alpha1.Proposal
	err := r.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &p)
	return &p, err
}

func approveProposal(t *testing.T, fc client.WithWatch, name string) {
	t.Helper()
	var p agenticv1alpha1.Proposal
	if err := fc.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &p); err != nil {
		t.Fatalf("get proposal for approval: %v", err)
	}
	base := p.DeepCopy()
	selected := int32(0)
	p.Status.Steps.Analysis.SelectedOption = &selected
	p.Status.Phase = agenticv1alpha1.ProposalPhaseApproved
	if err := fc.Status().Patch(context.Background(), &p, client.MergeFrom(base)); err != nil {
		t.Fatalf("approve: %v", err)
	}
}

// --- Reconciler-level tests ---

func TestReconcile_StatusInitialization(t *testing.T) {
	store := newTestStore(t)
	seedRequestContent(t, store, "fix-crash-request", "Pod crashing")

	scheme := testScheme()
	proposal := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fresh", Namespace: "default"},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:     agenticv1alpha1.ContentReference{Name: "fix-crash-request"},
			WorkflowRef: corev1.LocalObjectReference{Name: "remediation"},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		proposal, fullWorkflow(), testAnalyzerAgent(), testExecutorAgent(), testVerifierAgent(),
		testLLM("smart"), testLLM("fast"),
	).WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: newTestAgentCaller()}

	_, err := reconcileOnce(r, "fresh")
	if err != nil {
		t.Fatalf("reconcile on nil status: %v", err)
	}

	p, _ := getProposal(r, "fresh")
	if p.Status == nil {
		t.Fatal("status not initialized")
	}
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		t.Fatalf("expected Proposed, got %s", p.Status.Phase)
	}
	if p.Status.Attempt == nil || *p.Status.Attempt != 1 {
		t.Fatal("attempt not initialized to 1")
	}
}

func TestReconcile_Denied_Terminal(t *testing.T) {
	store := newTestStore(t)
	scheme := testScheme()

	one := int32(1)
	proposal := testProposal("remediation")
	proposal.Status = &agenticv1alpha1.ProposalStatus{
		Phase:   agenticv1alpha1.ProposalPhaseDenied,
		Attempt: &one,
		Steps:   &agenticv1alpha1.StepsStatus{},
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(proposal).WithStatusSubresource(proposal).Build()
	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Content: store, Agent: newTestAgentCaller()}

	result, err := reconcileOnce(r, "fix-crash")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Requeue {
		t.Error("terminal phase should not requeue")
	}
	p, _ := getProposal(r, "fix-crash")
	if p.Status.Phase != agenticv1alpha1.ProposalPhaseDenied {
		t.Fatalf("expected Denied, got %s", p.Status.Phase)
	}
}
