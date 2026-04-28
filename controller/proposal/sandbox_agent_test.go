package proposal

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

// --- Hand-written mocks ---

type mockSandboxProvider struct {
	claimName    string
	claimErr     error
	endpoint     string
	readyErr     error
	releaseErr   error
	claimCalls   int
	releaseCalls int
}

func (m *mockSandboxProvider) Claim(_ context.Context, _, _, _ string) (string, error) {
	m.claimCalls++
	return m.claimName, m.claimErr
}
func (m *mockSandboxProvider) WaitReady(_ context.Context, _ string, _ time.Duration) (string, error) {
	return m.endpoint, m.readyErr
}
func (m *mockSandboxProvider) Release(_ context.Context, _ string) error {
	m.releaseCalls++
	return m.releaseErr
}

type mockHTTPClient struct {
	response *agentQueryResponse
	err          error
	lastPhase    string
	lastQuery    string
	lastPrompt   string
	lastCtx      *agentContext
}

func (m *mockHTTPClient) Query(_ context.Context, phase, systemPrompt, query string, _ json.RawMessage, agentCtx *agentContext) (*agentQueryResponse, error) {
	m.lastPhase = phase
	m.lastQuery = query
	m.lastPrompt = systemPrompt
	m.lastCtx = agentCtx
	return m.response, m.err
}

func newTestSandboxAgentCaller(sandbox *mockSandboxProvider, httpClient *mockHTTPClient) *SandboxAgentCaller {
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	_ = fc.Create(context.Background(), fakeBaseTemplate())
	return &SandboxAgentCaller{
		Sandbox:          sandbox,
		K8sClient:        fc,
		ClientFactory:    func(_ string) AgentHTTPClientInterface { return httpClient },
		Namespace:        "test-ns",
		BaseTemplateName: "test-template",
		Timeout:          5 * time.Minute,
	}
}

func testSandboxProposal() *agenticv1alpha1.Proposal {
	p := testProposal("remediation")
	one := int32(1)
	p.Status.Attempt = &one
	return p
}

func testSandboxStep() resolvedStep {
	tools := testTools()
	return resolvedStep{
		Agent: testDefaultAgent(),
		LLM:   testLLM("smart"),
		Tools: &tools,
	}
}

// --- Happy path tests ---

func TestSandboxAgentCaller_Analyze_HappyPath(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "ls-analysis-fix-crash", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{
			Response: json.RawMessage(`{"options": [{"title": "Increase memory", "diagnosis": {"summary": "OOM", "confidence": "High", "rootCause": "memory limit"}, "proposal": {"description": "Bump memory", "actions": [{"type": "patch", "description": "patch deploy"}], "risk": "Low"}}]}`),
		},
	}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	result, err := caller.Analyze(context.Background(), testSandboxProposal(), testSandboxStep(), "Pod crashing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Options) != 1 {
		t.Fatalf("expected 1 option, got %d", len(result.Options))
	}
	if result.Options[0].Title != "Increase memory" {
		t.Errorf("title = %q", result.Options[0].Title)
	}
	if result.Options[0].Diagnosis.Confidence != "High" {
		t.Errorf("confidence = %q", result.Options[0].Diagnosis.Confidence)
	}
}

func TestSandboxAgentCaller_Execute_HappyPath(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "ls-execution-fix-crash", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{
			Response: json.RawMessage(`{"actionsTaken": [{"type": "patch", "description": "Patched deployment", "outcome": "Succeeded"}], "verification": {"conditionOutcome": "Improved", "summary": "Pod running"}}`),
		},
	}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	option := &agenticv1alpha1.RemediationOption{Title: "Fix it"}
	result, err := caller.Execute(context.Background(), testSandboxProposal(), testSandboxStep(), option)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ActionsTaken) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.ActionsTaken))
	}
	if result.ActionsTaken[0].Outcome != agenticv1alpha1.ActionOutcomeSucceeded {
		t.Errorf("outcome = %q", result.ActionsTaken[0].Outcome)
	}
	if result.Verification.ConditionOutcome != agenticv1alpha1.ConditionOutcomeImproved {
		t.Errorf("conditionOutcome = %q", result.Verification.ConditionOutcome)
	}
}

func TestSandboxAgentCaller_Verify_HappyPath(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "ls-verification-fix-crash", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{
			Response: json.RawMessage(`{"checks": [{"name": "pod-running", "source": "oc", "value": "Running", "result": "Passed"}], "summary": "All checks passed"}`),
		},
	}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	result, err := caller.Verify(context.Background(), testSandboxProposal(), testSandboxStep(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(result.Checks))
	}
	if result.Checks[0].Result != agenticv1alpha1.CheckResultPassed {
		t.Errorf("result = %q", result.Checks[0].Result)
	}
	if result.Summary != "All checks passed" {
		t.Errorf("summary = %q", result.Summary)
	}
}

// --- Error handling tests ---

func TestSandboxAgentCaller_ClaimError(t *testing.T) {
	sandbox := &mockSandboxProvider{claimErr: fmt.Errorf("quota exceeded")}
	httpClient := &mockHTTPClient{}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	_, err := caller.Analyze(context.Background(), testSandboxProposal(), testSandboxStep(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if httpClient.lastPhase != "" {
		t.Error("HTTP client should not have been called")
	}
	if sandbox.releaseCalls != 0 {
		t.Errorf("Release should not be called on Claim failure, got %d calls", sandbox.releaseCalls)
	}
}

func TestSandboxAgentCaller_WaitReadyError(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "claim-1", readyErr: fmt.Errorf("timeout")}
	httpClient := &mockHTTPClient{}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	_, err := caller.Execute(context.Background(), testSandboxProposal(), testSandboxStep(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if sandbox.releaseCalls != 1 {
		t.Errorf("Release should be called via defer, got %d calls", sandbox.releaseCalls)
	}
}

func TestSandboxAgentCaller_HTTPError(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "claim-1", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{err: fmt.Errorf("connection refused")}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	_, err := caller.Verify(context.Background(), testSandboxProposal(), testSandboxStep(), nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if sandbox.releaseCalls != 1 {
		t.Errorf("Release should be called via defer, got %d calls", sandbox.releaseCalls)
	}
}

func TestSandboxAgentCaller_ParseError(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "claim-1", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{Response: json.RawMessage("not valid json")},
	}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	_, err := caller.Analyze(context.Background(), testSandboxProposal(), testSandboxStep(), "test")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if sandbox.releaseCalls != 1 {
		t.Errorf("Release should be called via defer, got %d calls", sandbox.releaseCalls)
	}
}

func TestSandboxAgentCaller_ReleaseAlwaysCalled(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "claim-1", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{Response: json.RawMessage(`{"options": []}`)},
	}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	_, _ = caller.Analyze(context.Background(), testSandboxProposal(), testSandboxStep(), "test")

	if sandbox.claimCalls != 1 {
		t.Errorf("Claim calls = %d, want 1", sandbox.claimCalls)
	}
	if sandbox.releaseCalls != 1 {
		t.Errorf("Release calls = %d, want 1", sandbox.releaseCalls)
	}
}

// --- Phase and context propagation tests ---

func TestSandboxAgentCaller_CorrectPhase(t *testing.T) {
	tests := []struct {
		name  string
		phase string
		call  func(*SandboxAgentCaller) error
	}{
		{
			name:  "Analyze",
			phase: "analysis",
			call: func(c *SandboxAgentCaller) error {
				_, err := c.Analyze(context.Background(), testSandboxProposal(), testSandboxStep(), "test")
				return err
			},
		},
		{
			name:  "Execute",
			phase: "execution",
			call: func(c *SandboxAgentCaller) error {
				_, err := c.Execute(context.Background(), testSandboxProposal(), testSandboxStep(), nil)
				return err
			},
		},
		{
			name:  "Verify",
			phase: "verification",
			call: func(c *SandboxAgentCaller) error {
				_, err := c.Verify(context.Background(), testSandboxProposal(), testSandboxStep(), nil, nil)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox := &mockSandboxProvider{claimName: "claim-1", endpoint: "http://sandbox:8080"}
			httpClient := &mockHTTPClient{
				response: &agentQueryResponse{Response: json.RawMessage(`{"options":[],"checks":[],"actionsTaken":[],"summary":""}`)},
			}
			caller := newTestSandboxAgentCaller(sandbox, httpClient)
			_ = tt.call(caller)
			if httpClient.lastPhase != tt.phase {
				t.Errorf("phase = %q, want %q", httpClient.lastPhase, tt.phase)
			}
		})
	}
}

func TestSandboxAgentCaller_ContextPropagation(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "claim-1", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{Response: json.RawMessage(`{"options": []}`)},
	}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)

	attempt := int32(2)
	proposal := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fix-crash", Namespace: "default"},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:          "Pod crashing",
			TemplateRef:      &agenticv1alpha1.ProposalTemplateReference{Name: "remediation"},
			Tools:            testTools(),
			TargetNamespaces: []string{"production", "staging"},
		},
		Status: agenticv1alpha1.ProposalStatus{
			Attempt: &attempt,
			PreviousAttempts: []agenticv1alpha1.PreviousAttempt{
				{Attempt: 1, FailureReason: "analysis timeout"},
			},
		},
	}

	_, _ = caller.Analyze(context.Background(), proposal, testSandboxStep(), "test")

	if httpClient.lastCtx == nil {
		t.Fatal("expected context to be set")
	}
	if len(httpClient.lastCtx.TargetNamespaces) != 2 {
		t.Errorf("targetNamespaces count = %d, want 2", len(httpClient.lastCtx.TargetNamespaces))
	}
	if httpClient.lastCtx.Attempt != 2 {
		t.Errorf("attempt = %d, want 2", httpClient.lastCtx.Attempt)
	}
	if len(httpClient.lastCtx.PreviousAttempts) != 1 {
		t.Fatalf("previousAttempts count = %d, want 1", len(httpClient.lastCtx.PreviousAttempts))
	}
	if httpClient.lastCtx.PreviousAttempts[0].FailureReason != "analysis timeout" {
		t.Errorf("failureReason = %q", httpClient.lastCtx.PreviousAttempts[0].FailureReason)
	}
}

func TestSandboxAgentCaller_ExecutePassesApprovedOption(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "claim-1", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{Response: json.RawMessage(`{"actionsTaken": []}`)},
	}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	option := &agenticv1alpha1.RemediationOption{Title: "Scale up replicas"}
	_, _ = caller.Execute(context.Background(), testSandboxProposal(), testSandboxStep(), option)

	if httpClient.lastCtx == nil || httpClient.lastCtx.ApprovedOption == nil {
		t.Fatal("expected approved option in context")
	}
	if httpClient.lastCtx.ApprovedOption.Title != "Scale up replicas" {
		t.Errorf("approvedOption.title = %q", httpClient.lastCtx.ApprovedOption.Title)
	}
}
