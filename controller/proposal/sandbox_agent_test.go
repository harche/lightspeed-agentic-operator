package proposal

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func newTestSandboxAgentCallerWithProposal(sandbox *mockSandboxProvider, httpClient *mockHTTPClient, proposal *agenticv1alpha1.Proposal) *SandboxAgentCaller {
	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(proposal).
		WithStatusSubresource(proposal).
		Build()
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
			Response: json.RawMessage(`{"success": true, "options": [{"title": "Increase memory", "diagnosis": {"summary": "OOM", "confidence": "High", "rootCause": "memory limit"}, "proposal": {"description": "Bump memory", "actions": [{"type": "patch", "description": "patch deploy"}], "risk": "Low"}}]}`),
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
			Response: json.RawMessage(`{"success": true, "actionsTaken": [{"type": "patch", "description": "Patched deployment", "outcome": "Succeeded"}], "verification": {"conditionOutcome": "Improved", "summary": "Pod running"}}`),
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
			Response: json.RawMessage(`{"success": true, "checks": [{"name": "pod-running", "source": "oc", "value": "Running", "result": "Passed"}], "summary": "All checks passed"}`),
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
	if sandbox.releaseCalls != 0 {
		t.Errorf("Release calls = %d, want 0 (reconciler handles release)", sandbox.releaseCalls)
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
	if sandbox.releaseCalls != 0 {
		t.Errorf("Release calls = %d, want 0 (reconciler handles release)", sandbox.releaseCalls)
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
	if sandbox.releaseCalls != 0 {
		t.Errorf("Release calls = %d, want 0 (reconciler handles release)", sandbox.releaseCalls)
	}
}

func TestSandboxAgentCaller_SandboxNotReleasedAfterCall(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "claim-1", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{Response: json.RawMessage(`{"success": true, "options": []}`)},
	}

	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	_, _ = caller.Analyze(context.Background(), testSandboxProposal(), testSandboxStep(), "test")

	if sandbox.claimCalls != 1 {
		t.Errorf("Claim calls = %d, want 1", sandbox.claimCalls)
	}
	if sandbox.releaseCalls != 0 {
		t.Errorf("Release calls = %d, want 0 (reconciler handles release at terminal phase)", sandbox.releaseCalls)
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
				response: &agentQueryResponse{Response: json.RawMessage(`{"success": true, "options":[],"checks":[],"actionsTaken":[],"summary":""}`)},
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
		response: &agentQueryResponse{Response: json.RawMessage(`{"success": true, "options": []}`)},
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
		response: &agentQueryResponse{Response: json.RawMessage(`{"success": true, "actionsTaken": []}`)},
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

// --- Sandbox info patching tests ---

func TestSandboxAgentCaller_Analyze_PatchesSandboxInfo(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "ls-analysis-fix-crash", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{
			Response: json.RawMessage(`{"success": true, "options": [{"title": "Fix it", "diagnosis": {"summary": "broken", "confidence": "High", "rootCause": "bug"}, "proposal": {"description": "fix", "actions": [{"type": "patch", "description": "patch"}], "risk": "Low"}}]}`),
		},
	}

	proposal := testSandboxProposal()
	caller := newTestSandboxAgentCallerWithProposal(sandbox, httpClient, proposal)

	_, err := caller.Analyze(context.Background(), proposal, testSandboxStep(), "Pod crashing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated agenticv1alpha1.Proposal
	if err := caller.K8sClient.Get(context.Background(), client.ObjectKeyFromObject(proposal), &updated); err != nil {
		t.Fatalf("get proposal: %v", err)
	}

	if updated.Status.Steps.Analysis.Sandbox.ClaimName != "ls-analysis-fix-crash" {
		t.Errorf("sandbox claimName = %q, want %q", updated.Status.Steps.Analysis.Sandbox.ClaimName, "ls-analysis-fix-crash")
	}
	if updated.Status.Steps.Analysis.Sandbox.Namespace != "test-ns" {
		t.Errorf("sandbox namespace = %q, want %q", updated.Status.Steps.Analysis.Sandbox.Namespace, "test-ns")
	}
	if updated.Status.Steps.Analysis.Sandbox.StartTime == nil {
		t.Error("sandbox startTime should be set")
	}
}

func TestSandboxAgentCaller_Execute_PatchesSandboxInfo(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "ls-execution-fix-crash", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{
			Response: json.RawMessage(`{"success": true, "actionsTaken": [{"type": "patch", "description": "patched deploy"}]}`),
		},
	}

	proposal := testSandboxProposal()
	caller := newTestSandboxAgentCallerWithProposal(sandbox, httpClient, proposal)

	_, err := caller.Execute(context.Background(), proposal, testSandboxStep(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated agenticv1alpha1.Proposal
	if err := caller.K8sClient.Get(context.Background(), client.ObjectKeyFromObject(proposal), &updated); err != nil {
		t.Fatalf("get proposal: %v", err)
	}

	if updated.Status.Steps.Execution.Sandbox.ClaimName != "ls-execution-fix-crash" {
		t.Errorf("sandbox claimName = %q, want %q", updated.Status.Steps.Execution.Sandbox.ClaimName, "ls-execution-fix-crash")
	}
}

func TestSandboxAgentCaller_Verify_PatchesSandboxInfo(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "ls-verification-fix-crash", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{
			Response: json.RawMessage(`{"success": true, "checks": [{"name": "pod-running", "source": "oc", "value": "Running", "result": "Passed"}], "summary": "All checks passed"}`),
		},
	}

	proposal := testSandboxProposal()
	caller := newTestSandboxAgentCallerWithProposal(sandbox, httpClient, proposal)

	_, err := caller.Verify(context.Background(), proposal, testSandboxStep(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated agenticv1alpha1.Proposal
	if err := caller.K8sClient.Get(context.Background(), client.ObjectKeyFromObject(proposal), &updated); err != nil {
		t.Fatalf("get proposal: %v", err)
	}

	if updated.Status.Steps.Verification.Sandbox.ClaimName != "ls-verification-fix-crash" {
		t.Errorf("sandbox claimName = %q, want %q", updated.Status.Steps.Verification.Sandbox.ClaimName, "ls-verification-fix-crash")
	}
}

func TestSandboxAgentCaller_SandboxInfoPatch_DoesNotBlockOnError(t *testing.T) {
	sandbox := &mockSandboxProvider{claimName: "ls-analysis-fix-crash", endpoint: "http://sandbox:8080"}
	httpClient := &mockHTTPClient{
		response: &agentQueryResponse{
			Response: json.RawMessage(`{"success": true, "options": []}`),
		},
	}

	// Use caller WITHOUT proposal in the fake client — patchSandboxInfo will fail to Get
	// but the analysis call should still succeed
	caller := newTestSandboxAgentCaller(sandbox, httpClient)
	proposal := testSandboxProposal()

	_, err := caller.Analyze(context.Background(), proposal, testSandboxStep(), "test")
	if err != nil {
		t.Fatalf("analysis should succeed even when sandbox info patch fails: %v", err)
	}
}

func TestReleaseSandboxes_ReleasesAllSteps(t *testing.T) {
	releasedClaims := []string{}
	tracker := &trackingMockSandbox{released: &releasedClaims}

	caller := &SandboxAgentCaller{
		Sandbox:   tracker,
		Namespace: "test-ns",
	}

	proposal := &agenticv1alpha1.Proposal{
		Status: agenticv1alpha1.ProposalStatus{
			Steps: agenticv1alpha1.StepsStatus{
				Analysis: agenticv1alpha1.AnalysisStepStatus{
					Sandbox: agenticv1alpha1.SandboxInfo{ClaimName: "claim-analysis"},
				},
				Execution: agenticv1alpha1.ExecutionStepStatus{
					Sandbox: agenticv1alpha1.SandboxInfo{ClaimName: "claim-execution"},
				},
				Verification: agenticv1alpha1.VerificationStepStatus{
					Sandbox: agenticv1alpha1.SandboxInfo{ClaimName: "claim-verification"},
				},
			},
		},
	}

	err := caller.ReleaseSandboxes(context.Background(), proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(*tracker.released) != 3 {
		t.Fatalf("expected 3 releases, got %d", len(*tracker.released))
	}
	expected := []string{"claim-analysis", "claim-execution", "claim-verification"}
	for i, name := range expected {
		if (*tracker.released)[i] != name {
			t.Errorf("release[%d] = %q, want %q", i, (*tracker.released)[i], name)
		}
	}
}

func TestReleaseSandboxes_SkipsEmptyClaims(t *testing.T) {
	releasedClaims := []string{}
	tracker := &trackingMockSandbox{released: &releasedClaims}

	caller := &SandboxAgentCaller{Sandbox: tracker, Namespace: "test-ns"}

	proposal := &agenticv1alpha1.Proposal{
		Status: agenticv1alpha1.ProposalStatus{
			Steps: agenticv1alpha1.StepsStatus{
				Analysis: agenticv1alpha1.AnalysisStepStatus{
					Sandbox: agenticv1alpha1.SandboxInfo{ClaimName: "claim-analysis"},
				},
			},
		},
	}

	err := caller.ReleaseSandboxes(context.Background(), proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(*tracker.released) != 1 {
		t.Fatalf("expected 1 release, got %d", len(*tracker.released))
	}
}

func TestReleaseSandboxes_ContinuesOnError(t *testing.T) {
	releasedClaims := []string{}
	tracker := &trackingMockSandbox{
		released: &releasedClaims,
		errOnClaim: "claim-execution",
	}

	caller := &SandboxAgentCaller{Sandbox: tracker, Namespace: "test-ns"}

	proposal := &agenticv1alpha1.Proposal{
		Status: agenticv1alpha1.ProposalStatus{
			Steps: agenticv1alpha1.StepsStatus{
				Analysis: agenticv1alpha1.AnalysisStepStatus{
					Sandbox: agenticv1alpha1.SandboxInfo{ClaimName: "claim-analysis"},
				},
				Execution: agenticv1alpha1.ExecutionStepStatus{
					Sandbox: agenticv1alpha1.SandboxInfo{ClaimName: "claim-execution"},
				},
				Verification: agenticv1alpha1.VerificationStepStatus{
					Sandbox: agenticv1alpha1.SandboxInfo{ClaimName: "claim-verification"},
				},
			},
		},
	}

	err := caller.ReleaseSandboxes(context.Background(), proposal)
	if err == nil {
		t.Fatal("expected error from failing release")
	}
	// Should still attempt all three
	if len(*tracker.released) != 3 {
		t.Fatalf("expected 3 release attempts, got %d", len(*tracker.released))
	}
}

type trackingMockSandbox struct {
	released   *[]string
	errOnClaim string
}

func (m *trackingMockSandbox) Claim(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (m *trackingMockSandbox) WaitReady(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", nil
}
func (m *trackingMockSandbox) Release(_ context.Context, claimName string) error {
	*m.released = append(*m.released, claimName)
	if m.errOnClaim != "" && claimName == m.errOnClaim {
		return fmt.Errorf("simulated release error for %s", claimName)
	}
	return nil
}
