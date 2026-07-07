package proposal

import (
	"strings"
	"testing"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

func TestBuildVerificationQuery_DiagnosisOnlyWithoutExecution(t *testing.T) {
	opt := &agenticv1alpha1.RemediationOption{Title: "opt"}

	withExec := buildVerificationQuery(opt, &ExecutionOutput{Success: true})
	if !strings.Contains(withExec, "executed remediation") || !strings.Contains(withExec, "## Execution Result") {
		t.Errorf("with execution: expected remediation-verification framing, got:\n%s", withExec)
	}

	withoutExec := buildVerificationQuery(opt, nil)
	if !strings.Contains(withoutExec, "verify the DIAGNOSIS") {
		t.Errorf("without execution: expected diagnosis-verification framing, got:\n%s", withoutExec)
	}
	if strings.Contains(withoutExec, "## Execution Result") {
		t.Errorf("without execution: must not include an Execution Result section")
	}
}

func TestBuildExecutionQuery_RetryFeedback(t *testing.T) {
	opt := &agenticv1alpha1.RemediationOption{Title: "opt"}

	first := buildExecutionQuery(opt, "")
	if strings.Contains(first, "Previous Attempt Feedback") {
		t.Errorf("first attempt must not include retry feedback section, got:\n%s", first)
	}

	retry := buildExecutionQuery(opt, "Verification failed (attempt 2/3): probe on / returns 403")
	if !strings.Contains(retry, "Previous Attempt Feedback") || !strings.Contains(retry, "returns 403") {
		t.Errorf("retry attempt must include the verification failure feedback, got:\n%s", retry)
	}
}
