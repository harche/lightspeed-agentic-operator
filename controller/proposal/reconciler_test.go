package proposal

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

// --- Configurable agent stub for tests ---

type testAgentCaller struct {
	analyzeErr error
	executeErr error
	verifyErr  error

	analyzeResult *AnalysisOutput
	executeResult *ExecutionOutput
	verifyResult  *VerificationOutput
}

func newTestAgentCaller() *testAgentCaller {
	stub := &StubAgentCaller{}
	a, _ := stub.Analyze(context.Background(), nil, resolvedStep{}, "")
	e, _ := stub.Execute(context.Background(), nil, resolvedStep{}, nil)
	v, _ := stub.Verify(context.Background(), nil, resolvedStep{}, nil, nil)
	return &testAgentCaller{analyzeResult: a, executeResult: e, verifyResult: v}
}

func (ta *testAgentCaller) Analyze(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ string) (*AnalysisOutput, error) {
	if ta.analyzeErr != nil {
		return nil, ta.analyzeErr
	}
	return ta.analyzeResult, nil
}
func (ta *testAgentCaller) Execute(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RemediationOption) (*ExecutionOutput, error) {
	if ta.executeErr != nil {
		return nil, ta.executeErr
	}
	return ta.executeResult, nil
}
func (ta *testAgentCaller) Verify(_ context.Context, _ *agenticv1alpha1.Proposal, _ resolvedStep, _ *agenticv1alpha1.RemediationOption, _ *ExecutionOutput) (*VerificationOutput, error) {
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

func fullWorkflow() *agenticv1alpha1.Workflow {
	return &agenticv1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "remediation", Namespace: "default"},
		Spec: agenticv1alpha1.WorkflowSpec{
			Analysis: agenticv1alpha1.WorkflowStep{
				Agent:          "default",
				ComponentTools: agenticv1alpha1.ComponentToolsReference{Name: "test-tools"},
			},
			Execution: agenticv1alpha1.WorkflowStep{
				Agent:          "default",
				ComponentTools: agenticv1alpha1.ComponentToolsReference{Name: "test-tools"},
			},
			Verification: agenticv1alpha1.WorkflowStep{
				Agent:          "default",
				ComponentTools: agenticv1alpha1.ComponentToolsReference{Name: "test-tools"},
			},
		},
	}
}

func testDefaultAgent() *agenticv1alpha1.Agent {
	return &agenticv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: agenticv1alpha1.AgentSpec{
			LLMProvider: agenticv1alpha1.LLMProviderReference{Name: "smart"},
		},
	}
}

func testComponentTools() *agenticv1alpha1.ComponentTools {
	return &agenticv1alpha1.ComponentTools{
		ObjectMeta: metav1.ObjectMeta{Name: "test-tools", Namespace: "default"},
		Spec: agenticv1alpha1.ComponentToolsSpec{
			Skills: []agenticv1alpha1.SkillsSource{{Image: "registry.example.com/skills:latest"}},
		},
	}
}

func testLLM(name string) *agenticv1alpha1.LLMProvider {
	return &agenticv1alpha1.LLMProvider{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: agenticv1alpha1.LLMProviderSpec{
			Type:              agenticv1alpha1.LLMProviderVertex,
			Model:             "claude-opus-4-6",
			CredentialsSecret: agenticv1alpha1.NamespacedSecretReference{Name: "llm-secret", Namespace: "lightspeed"},
		},
	}
}

func testProposal(workflowName string) *agenticv1alpha1.Proposal {
	return &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fix-crash", Namespace: "default"},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:          "Pod crashing in production",
			Workflow:         agenticv1alpha1.WorkflowReference{Name: workflowName},
			TargetNamespaces: []string{"production"},
		},
	}
}

// defaultObjects returns the standard set of cluster-scoped and namespaced
// objects needed to resolve a full workflow.
func defaultObjects() []client.Object {
	return []client.Object{
		testDefaultAgent(), testLLM("smart"), testComponentTools(),
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

// --- Sandbox-based reconciler helpers ---

func newMockSandboxAgent(analysisJSON, executionJSON, verificationJSON string) (*SandboxAgentCaller, *mockSandboxProvider) {
	sandbox := &mockSandboxProvider{claimName: "ls-test-claim", endpoint: "http://sandbox:8080"}

	callCount := 0
	responses := []string{analysisJSON, executionJSON, verificationJSON}

	httpClient := &mockHTTPClient{}
	caller := &SandboxAgentCaller{
		Sandbox: sandbox,
		ClientFactory: func(_ string) AgentHTTPClientInterface {
			resp := responses[callCount%len(responses)]
			callCount++
			httpClient.response = &agentQueryResponse{Response: json.RawMessage(resp)}
			return httpClient
		},
		Namespace:    "test-ns",
		TemplateName: "test-template",
		Timeout:      5 * time.Minute,
	}
	return caller, sandbox
}

// --- Reconciler-level tests ---

func TestReconcile_StatusInitialization(t *testing.T) {
	scheme := testScheme()
	proposal := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fresh", Namespace: "default"},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:  "Pod crashing",
			Workflow: agenticv1alpha1.WorkflowReference{Name: "remediation"},
		},
	}

	objs := append([]client.Object{proposal, fullWorkflow()}, defaultObjects()...)
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(proposal).Build()

	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: newTestAgentCaller()}

	_, err := reconcileOnce(r, "fresh")
	if err != nil {
		t.Fatalf("reconcile on nil status: %v", err)
	}

	p, _ := getProposal(r, "fresh")
	if p.Status.Phase == "" {
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
	scheme := testScheme()

	one := int32(1)
	proposal := testProposal("remediation")
	proposal.Status = agenticv1alpha1.ProposalStatus{
		Phase:   agenticv1alpha1.ProposalPhaseDenied,
		Attempt: &one,
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(proposal).WithStatusSubresource(proposal).Build()
	r := &ProposalReconciler{Client: fc, Log: logr.Discard(), Agent: newTestAgentCaller()}

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
