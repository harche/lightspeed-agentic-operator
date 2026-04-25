/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package proposal

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

// TestProposalLifecycleWithContentStore simulates the operator's full
// proposal lifecycle using the PostgreSQL-backed ContentStore. This is
// the actual code path the reconciler takes in production.
//
// Flow:
//  1. Adapter creates request content → Proposal.Spec.Request references it
//  2. Operator reads request content via ContentStore
//  3. Call analysis agent → store result via ContentStore → set ref on Proposal
//  4. User selects option → operator reads AnalysisResult back for RBAC + plan
//  5. Call execution agent → store result via ContentStore → set ref
//  6. Call verification agent → store result via ContentStore → set ref
func TestProposalLifecycleWithContentStore(t *testing.T) {
	ctx := context.Background()
	var store ContentStore = newTestStore(t)

	// --- Setup: adapter creates request content ---
	requestSpec := v1alpha1.RequestContentSpec{
		ContentPayload: v1alpha1.ContentPayload{
			MediaType: "text/plain",
			Content:   "Pod web-frontend-5d4b8c6f in namespace production is in CrashLoopBackOff. Last restart reason: OOMKilled. Container memory limit is 256Mi.",
		},
	}
	if err := store.CreateRequestContent(ctx, "fix-crashloop-request", requestSpec); err != nil {
		t.Fatalf("create request content: %v", err)
	}

	proposal := &v1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fix-crashloop", Namespace: "openshift-lightspeed"},
		Spec: v1alpha1.ProposalSpec{
			Request:          v1alpha1.ContentReference{Name: "fix-crashloop-request"},
			WorkflowRef:      corev1.LocalObjectReference{Name: "remediation"},
			TargetNamespaces: []string{"production"},
		},
		Status: &v1alpha1.ProposalStatus{
			Phase:   v1alpha1.ProposalPhasePending,
			Attempt: int32Ptr(1),
			Steps:   &v1alpha1.StepsStatus{},
		},
	}

	// --- Step 1: Operator reads request content via ContentStore ---
	t.Run("resolve_request_content", func(t *testing.T) {
		fetched, err := store.GetRequestContent(ctx, proposal.Spec.Request.Name)
		if err != nil {
			t.Fatalf("GetRequestContent: %v", err)
		}
		if fetched.Content == "" {
			t.Fatal("request content is empty")
		}
		t.Logf("request: %q", fetched.Content[:60])
	})

	// --- Step 2: Analysis agent returns result, operator stores it ---
	t.Run("store_and_read_analysis_result", func(t *testing.T) {
		reversible := true
		spec := v1alpha1.AnalysisResultSpec{
			Options: []v1alpha1.RemediationOption{{
				Title: "Increase memory limit",
				Diagnosis: v1alpha1.DiagnosisResult{
					Summary:    "OOMKilled due to 256Mi limit",
					Confidence: "High",
					RootCause:  "Memory limit too low",
				},
				Proposal: v1alpha1.ProposalResult{
					Description: "Increase memory from 256Mi to 512Mi",
					Actions:     []v1alpha1.ProposedAction{{Type: "patch", Description: "Patch deployment"}},
					Risk:        "Low",
					Reversible:  &reversible,
				},
				RBAC: &v1alpha1.RBACResult{
					NamespaceScoped: []v1alpha1.RBACRule{{
						APIGroups:     []string{"apps"},
						Resources:     []string{"deployments"},
						Verbs:         []string{"get", "patch"},
						Justification: "Patch deployment memory limit",
					}},
				},
				Verification: &v1alpha1.VerificationPlan{
					Description: "Verify pod is running",
					Steps: []v1alpha1.VerificationStep{{
						Name: "pod-running", Command: "oc get pod", Expected: "Running", Type: "command",
					}},
				},
			}},
		}

		resultName := fmt.Sprintf("%s-analysis", proposal.Name)
		if err := store.CreateAnalysisResult(ctx, resultName, spec); err != nil {
			t.Fatalf("CreateAnalysisResult: %v", err)
		}

		proposal.Status.Steps.Analysis = &v1alpha1.AnalysisStepStatus{
			Result: &v1alpha1.ContentReference{Name: resultName},
		}
		proposal.Status.Phase = v1alpha1.ProposalPhaseProposed

		// Read it back
		fetched, err := store.GetAnalysisResult(ctx, resultName)
		if err != nil {
			t.Fatalf("GetAnalysisResult: %v", err)
		}
		if len(fetched.Options) != 1 {
			t.Fatalf("options = %d, want 1", len(fetched.Options))
		}
		if fetched.Options[0].Diagnosis.Confidence != "High" {
			t.Errorf("confidence = %q", fetched.Options[0].Diagnosis.Confidence)
		}
	})

	// --- Step 3: User approves, operator reads RBAC from store ---
	t.Run("read_rbac_for_execution", func(t *testing.T) {
		selected := int32(0)
		proposal.Status.Steps.Analysis.SelectedOption = &selected

		result, err := store.GetAnalysisResult(ctx, proposal.Status.Steps.Analysis.Result.Name)
		if err != nil {
			t.Fatalf("GetAnalysisResult: %v", err)
		}

		option := result.Options[*proposal.Status.Steps.Analysis.SelectedOption]
		if option.RBAC == nil {
			t.Fatal("selected option has no RBAC")
		}
		if len(option.RBAC.NamespaceScoped) == 0 {
			t.Fatal("no namespace-scoped RBAC rules")
		}
		if option.RBAC.NamespaceScoped[0].Verbs[1] != "patch" {
			t.Errorf("expected patch verb, got %q", option.RBAC.NamespaceScoped[0].Verbs[1])
		}
	})

	// --- Step 4: Execution agent returns result, operator stores it ---
	t.Run("store_and_read_execution_result", func(t *testing.T) {
		success := true
		improved := true
		spec := v1alpha1.ExecutionResultSpec{
			ActionsTaken: []v1alpha1.ExecutionAction{
				{Type: "patch", Description: "Patched deployment/web memory to 512Mi", Success: &success},
			},
			Verification: &v1alpha1.ExecutionVerification{
				ConditionImproved: &improved,
				Summary:           "Pod running with new limit",
			},
		}

		resultName := fmt.Sprintf("%s-execution", proposal.Name)
		if err := store.CreateExecutionResult(ctx, resultName, spec); err != nil {
			t.Fatalf("CreateExecutionResult: %v", err)
		}

		proposal.Status.Steps.Execution = &v1alpha1.ExecutionStepStatus{
			Result: &v1alpha1.ContentReference{Name: resultName},
		}

		fetched, err := store.GetExecutionResult(ctx, resultName)
		if err != nil {
			t.Fatalf("GetExecutionResult: %v", err)
		}
		if !*fetched.ActionsTaken[0].Success {
			t.Error("action success lost")
		}
		if !*fetched.Verification.ConditionImproved {
			t.Error("condition improved lost")
		}
	})

	// --- Step 5: Verification agent returns result, operator stores it ---
	t.Run("store_and_read_verification_result", func(t *testing.T) {
		passed := true
		spec := v1alpha1.VerificationResultSpec{
			Checks: []v1alpha1.VerifyCheck{
				{Name: "pod-running", Source: "oc", Value: "Running", Passed: &passed},
			},
			Summary: "All checks passed",
		}

		resultName := fmt.Sprintf("%s-verification", proposal.Name)
		if err := store.CreateVerificationResult(ctx, resultName, spec); err != nil {
			t.Fatalf("CreateVerificationResult: %v", err)
		}

		proposal.Status.Steps.Verification = &v1alpha1.VerificationStepStatus{
			Result: &v1alpha1.ContentReference{Name: resultName},
		}
		proposal.Status.Phase = v1alpha1.ProposalPhaseCompleted

		fetched, err := store.GetVerificationResult(ctx, resultName)
		if err != nil {
			t.Fatalf("GetVerificationResult: %v", err)
		}
		if !*fetched.Checks[0].Passed {
			t.Error("check passed lost")
		}
		if fetched.Summary != "All checks passed" {
			t.Errorf("summary = %q", fetched.Summary)
		}
	})

	// --- Step 6: Proposal CR stays small ---
	t.Run("proposal_stays_small", func(t *testing.T) {
		data, err := json.Marshal(proposal)
		if err != nil {
			t.Fatalf("marshal proposal: %v", err)
		}
		t.Logf("proposal JSON size: %d bytes", len(data))
		if len(data) > 2048 {
			t.Errorf("proposal too large: %d bytes", len(data))
		}

		// Verify every ref resolves through the store
		if req, err := store.GetRequestContent(ctx, proposal.Spec.Request.Name); err != nil {
			t.Errorf("request ref broken: %v", err)
		} else if req.Content == "" && len(req.Data) == 0 {
			t.Error("request content is empty")
		}
		if _, err := store.GetAnalysisResult(ctx, proposal.Status.Steps.Analysis.Result.Name); err != nil {
			t.Errorf("analysis ref broken: %v", err)
		}
		if _, err := store.GetExecutionResult(ctx, proposal.Status.Steps.Execution.Result.Name); err != nil {
			t.Errorf("execution ref broken: %v", err)
		}
		if _, err := store.GetVerificationResult(ctx, proposal.Status.Steps.Verification.Result.Name); err != nil {
			t.Errorf("verification ref broken: %v", err)
		}
	})
}

// TestRetryWithContentStore verifies the retry flow preserves content
// references across attempts.
func TestRetryWithContentStore(t *testing.T) {
	ctx := context.Background()
	var store ContentStore = newTestStore(t)

	// First attempt's analysis result
	_ = store.CreateAnalysisResult(ctx, "fix-retry-analysis-1", v1alpha1.AnalysisResultSpec{
		Options: []v1alpha1.RemediationOption{{
			Title:     "First approach",
			Diagnosis: v1alpha1.DiagnosisResult{Summary: "diag", Confidence: "High", RootCause: "root"},
			Proposal:  v1alpha1.ProposalResult{Description: "plan", Risk: "Low"},
		}},
	})

	proposal := &v1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fix-retry"},
		Spec: v1alpha1.ProposalSpec{
			Request:     v1alpha1.ContentReference{Name: "fix-retry-request"},
			WorkflowRef: corev1.LocalObjectReference{Name: "remediation"},
		},
		Status: &v1alpha1.ProposalStatus{
			Phase:   v1alpha1.ProposalPhasePending,
			Attempt: int32Ptr(2),
			Steps:   &v1alpha1.StepsStatus{},
			PreviousAttempts: []v1alpha1.PreviousAttempt{{
				Attempt:       1,
				FailedStep:    sandboxStepPtr(v1alpha1.SandboxStepExecution),
				FailureReason: strPtr("admission webhook rejected patch"),
			}},
		},
	}

	// Previous attempt's result is still in the store
	old, err := store.GetAnalysisResult(ctx, "fix-retry-analysis-1")
	if err != nil {
		t.Fatalf("previous result not accessible: %v", err)
	}
	if old.Options[0].Title != "First approach" {
		t.Error("previous result corrupted")
	}

	// Failure history on the Proposal CR
	if *proposal.Status.PreviousAttempts[0].FailureReason != "admission webhook rejected patch" {
		t.Error("failure reason lost")
	}
}

// TestEscalationWithContentStore verifies child proposals can read
// parent content through the store.
func TestEscalationWithContentStore(t *testing.T) {
	ctx := context.Background()
	var store ContentStore = newTestStore(t)

	// Parent's results in the store
	_ = store.CreateRequestContent(ctx, "fix-parent-request", v1alpha1.RequestContentSpec{ContentPayload: v1alpha1.ContentPayload{Content: "Original alert"}})
	_ = store.CreateAnalysisResult(ctx, "fix-parent-analysis", v1alpha1.AnalysisResultSpec{
		Options: []v1alpha1.RemediationOption{{
			Title:     "Parent's approach",
			Diagnosis: v1alpha1.DiagnosisResult{Summary: "diag", Confidence: "High", RootCause: "root"},
			Proposal:  v1alpha1.ProposalResult{Description: "plan", Risk: "Medium"},
		}},
	})

	parent := &v1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fix-parent"},
		Spec: v1alpha1.ProposalSpec{
			Request:     v1alpha1.ContentReference{Name: "fix-parent-request"},
			WorkflowRef: corev1.LocalObjectReference{Name: "remediation"},
		},
		Status: &v1alpha1.ProposalStatus{
			Phase:   v1alpha1.ProposalPhaseEscalated,
			Attempt: int32Ptr(3),
			Steps: &v1alpha1.StepsStatus{
				Analysis: &v1alpha1.AnalysisStepStatus{
					Result: &v1alpha1.ContentReference{Name: "fix-parent-analysis"},
				},
			},
		},
	}

	// Operator reads parent's analysis result to build escalation context
	parentAnalysis, err := store.GetAnalysisResult(ctx, parent.Status.Steps.Analysis.Result.Name)
	if err != nil {
		t.Fatalf("can't read parent analysis: %v", err)
	}

	// Operator creates escalation request content
	escalationText := fmt.Sprintf("Escalation: failed %d attempts. Previous approach: %s",
		*parent.Status.Attempt, parentAnalysis.Options[0].Title)
	if err := store.CreateRequestContent(ctx, "fix-parent-escalation-request", v1alpha1.RequestContentSpec{ContentPayload: v1alpha1.ContentPayload{Content: escalationText}}); err != nil {
		t.Fatalf("create escalation request: %v", err)
	}

	// Child proposal references the escalation request
	child := &v1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fix-parent-escalation"},
		Spec: v1alpha1.ProposalSpec{
			Request:     v1alpha1.ContentReference{Name: "fix-parent-escalation-request"},
			WorkflowRef: parent.Spec.WorkflowRef,
			ParentRef:   &corev1.LocalObjectReference{Name: parent.Name},
		},
	}

	// Verify the child's request resolves
	fetched, err := store.GetRequestContent(ctx, child.Spec.Request.Name)
	if err != nil {
		t.Fatalf("child request ref broken: %v", err)
	}
	t.Logf("escalation request: %s", fetched.Content)
}

// TestRevisionLifecycleWithContentStore simulates the revision flow using
// the PostgreSQL-backed ContentStore. Traces the full path:
//  1. Adapter creates request content, operator runs initial analysis
//  2. User submits revision feedback as RequestContent
//  3. Operator re-runs analysis, stores revised result alongside the original
//  4. User submits second revision, operator stores a third result
//  5. User approves revised option, execution reads from the latest result
//  6. All prior results remain accessible in the store
func TestRevisionLifecycleWithContentStore(t *testing.T) {
	ctx := context.Background()
	var store ContentStore = newTestStore(t)

	// --- Setup: adapter creates request content ---
	requestSpec := v1alpha1.RequestContentSpec{
		ContentPayload: v1alpha1.ContentPayload{
			MediaType: "text/plain",
			Content:   "Pod app-server in namespace production is OOMKilled. Memory limit 500Mi. JVM heap peaks at 480MB. 3 restarts in 10 minutes.",
		},
	}
	if err := store.CreateRequestContent(ctx, "fix-jvm-oom-request", requestSpec); err != nil {
		t.Fatalf("create request content: %v", err)
	}

	reversible := true
	proposal := &v1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fix-jvm-oom", Namespace: "openshift-lightspeed"},
		Spec: v1alpha1.ProposalSpec{
			Request:          v1alpha1.ContentReference{Name: "fix-jvm-oom-request"},
			WorkflowRef:      corev1.LocalObjectReference{Name: "remediation"},
			TargetNamespaces: []string{"production"},
		},
		Status: &v1alpha1.ProposalStatus{
			Phase:   v1alpha1.ProposalPhasePending,
			Attempt: int32Ptr(1),
			Steps:   &v1alpha1.StepsStatus{},
		},
	}

	// --- Step 1: Initial analysis result stored ---
	t.Run("initial_analysis", func(t *testing.T) {
		spec := v1alpha1.AnalysisResultSpec{
			Options: []v1alpha1.RemediationOption{{
				Title: "Increase memory from 500MB to 768MB",
				Diagnosis: v1alpha1.DiagnosisResult{
					Summary:    "OOMKilled due to JVM heap peaking at 480MB against 500Mi limit",
					Confidence: "High",
					RootCause:  "Memory limit too low for JVM heap",
				},
				Proposal: v1alpha1.ProposalResult{
					Description: "Increase memory from 500Mi to 768Mi",
					Actions:     []v1alpha1.ProposedAction{{Type: "patch", Description: "Patch deployment memory limit to 768Mi"}},
					Risk:        "Low",
					Reversible:  &reversible,
				},
				RBAC: &v1alpha1.RBACResult{
					NamespaceScoped: []v1alpha1.RBACRule{{
						APIGroups:     []string{"apps"},
						Resources:     []string{"deployments"},
						Verbs:         []string{"get", "patch"},
						Justification: "Need to patch deployment memory limit",
					}},
				},
			}},
		}

		if err := store.CreateAnalysisResult(ctx, "fix-jvm-oom-analysis-1", spec); err != nil {
			t.Fatalf("CreateAnalysisResult: %v", err)
		}

		proposal.Status.Steps.Analysis = &v1alpha1.AnalysisStepStatus{
			Result: &v1alpha1.ContentReference{Name: "fix-jvm-oom-analysis-1"},
		}
		proposal.Status.Phase = v1alpha1.ProposalPhaseProposed

		fetched, err := store.GetAnalysisResult(ctx, "fix-jvm-oom-analysis-1")
		if err != nil {
			t.Fatalf("GetAnalysisResult: %v", err)
		}
		if fetched.Options[0].Title != "Increase memory from 500MB to 768MB" {
			t.Errorf("title = %q", fetched.Options[0].Title)
		}
	})

	// --- Step 2: User submits revision feedback ---
	t.Run("store_revision_feedback", func(t *testing.T) {
		feedbackSpec := v1alpha1.RequestContentSpec{
			ContentPayload: v1alpha1.ContentPayload{
				MediaType: "text/plain",
				Content:   "768MB is too conservative. The app has memory-heavy batch jobs at night. How about 1024MB?",
			},
		}
		if err := store.CreateRequestContent(ctx, "fix-jvm-oom-revision-1", feedbackSpec); err != nil {
			t.Fatalf("create revision feedback: %v", err)
		}

		// Verify feedback is readable
		fetched, err := store.GetRequestContent(ctx, "fix-jvm-oom-revision-1")
		if err != nil {
			t.Fatalf("GetRequestContent revision: %v", err)
		}
		if fetched.Content == "" {
			t.Fatal("revision feedback is empty")
		}

		proposal.Spec.Revision = int32Ptr(1)
	})

	// --- Step 3: Operator re-runs analysis, stores revised result ---
	t.Run("store_revised_analysis", func(t *testing.T) {
		spec := v1alpha1.AnalysisResultSpec{
			Options: []v1alpha1.RemediationOption{
				{
					Title: "Increase to 1024MB (as requested)",
					Diagnosis: v1alpha1.DiagnosisResult{
						Summary:    "OOMKilled. User requests 1024MB. Node allocatable allows it but with 15% less headroom.",
						Confidence: "High",
						RootCause:  "Memory limit too low for JVM heap",
					},
					Proposal: v1alpha1.ProposalResult{
						Description:     "Increase memory from 500Mi to 1024Mi",
						Actions:         []v1alpha1.ProposedAction{{Type: "patch", Description: "Patch deployment memory limit to 1024Mi"}},
						Risk:            "Medium",
						Reversible:      &reversible,
						EstimatedImpact: strPtr("Exceeds typical node headroom by 15%, may trigger eviction under peak"),
					},
					RBAC: &v1alpha1.RBACResult{
						NamespaceScoped: []v1alpha1.RBACRule{{
							APIGroups:     []string{"apps"},
							Resources:     []string{"deployments"},
							Verbs:         []string{"get", "patch"},
							Justification: "Need to patch deployment memory limit",
						}},
					},
				},
				{
					Title: "Increase to 768MB (recommended)",
					Diagnosis: v1alpha1.DiagnosisResult{
						Summary:    "OOMKilled. 768MB provides 30% headroom over observed peak, safe within node capacity.",
						Confidence: "High",
						RootCause:  "Memory limit too low for JVM heap",
					},
					Proposal: v1alpha1.ProposalResult{
						Description: "Increase memory from 500Mi to 768Mi",
						Actions:     []v1alpha1.ProposedAction{{Type: "patch", Description: "Patch deployment memory limit to 768Mi"}},
						Risk:        "Low",
						Reversible:  &reversible,
					},
					RBAC: &v1alpha1.RBACResult{
						NamespaceScoped: []v1alpha1.RBACRule{{
							APIGroups:     []string{"apps"},
							Resources:     []string{"deployments"},
							Verbs:         []string{"get", "patch"},
							Justification: "Need to patch deployment memory limit",
						}},
					},
				},
			},
		}

		if err := store.CreateAnalysisResult(ctx, "fix-jvm-oom-analysis-1-rev1", spec); err != nil {
			t.Fatalf("CreateAnalysisResult revised: %v", err)
		}

		proposal.Status.Steps.Analysis.Result = &v1alpha1.ContentReference{Name: "fix-jvm-oom-analysis-1-rev1"}
		proposal.Status.Steps.Analysis.ObservedRevision = int32Ptr(1)

		// Read revised result
		fetched, err := store.GetAnalysisResult(ctx, "fix-jvm-oom-analysis-1-rev1")
		if err != nil {
			t.Fatalf("GetAnalysisResult revised: %v", err)
		}
		if len(fetched.Options) != 2 {
			t.Fatalf("expected 2 options, got %d", len(fetched.Options))
		}
		if fetched.Options[0].Proposal.Risk != "Medium" {
			t.Errorf("option 0 risk = %q, want Medium", fetched.Options[0].Proposal.Risk)
		}
		if fetched.Options[1].Proposal.Risk != "Low" {
			t.Errorf("option 1 risk = %q, want Low", fetched.Options[1].Proposal.Risk)
		}
	})

	// --- Step 4: Second revision round ---
	t.Run("second_revision", func(t *testing.T) {
		feedbackSpec := v1alpha1.RequestContentSpec{
			ContentPayload: v1alpha1.ContentPayload{
				Content: "What about 896MB as a compromise?",
			},
		}
		if err := store.CreateRequestContent(ctx, "fix-jvm-oom-revision-2", feedbackSpec); err != nil {
			t.Fatalf("create second revision feedback: %v", err)
		}

		spec := v1alpha1.AnalysisResultSpec{
			Options: []v1alpha1.RemediationOption{{
				Title: "Increase to 896MB (compromise)",
				Diagnosis: v1alpha1.DiagnosisResult{
					Summary:    "896MB covers batch peak with 20% headroom, safe within node capacity.",
					Confidence: "High",
					RootCause:  "Memory limit too low for JVM heap",
				},
				Proposal: v1alpha1.ProposalResult{
					Description: "Increase memory from 500Mi to 896Mi",
					Actions:     []v1alpha1.ProposedAction{{Type: "patch", Description: "Patch deployment memory limit to 896Mi"}},
					Risk:        "Low",
					Reversible:  &reversible,
				},
			}},
		}

		if err := store.CreateAnalysisResult(ctx, "fix-jvm-oom-analysis-1-rev2", spec); err != nil {
			t.Fatalf("CreateAnalysisResult rev2: %v", err)
		}

		proposal.Spec.Revision = int32Ptr(2)
		proposal.Status.Steps.Analysis.Result = &v1alpha1.ContentReference{Name: "fix-jvm-oom-analysis-1-rev2"}
		proposal.Status.Steps.Analysis.ObservedRevision = int32Ptr(2)
	})

	// --- Step 5: All prior results remain accessible ---
	t.Run("all_results_accessible", func(t *testing.T) {
		names := []string{
			"fix-jvm-oom-analysis-1",
			"fix-jvm-oom-analysis-1-rev1",
			"fix-jvm-oom-analysis-1-rev2",
		}
		for _, name := range names {
			if _, err := store.GetAnalysisResult(ctx, name); err != nil {
				t.Errorf("result %q not accessible: %v", name, err)
			}
		}

		// Revision feedback also accessible
		for _, name := range []string{"fix-jvm-oom-revision-1", "fix-jvm-oom-revision-2"} {
			if _, err := store.GetRequestContent(ctx, name); err != nil {
				t.Errorf("revision feedback %q not accessible: %v", name, err)
			}
		}

		// Original request still accessible
		if _, err := store.GetRequestContent(ctx, "fix-jvm-oom-request"); err != nil {
			t.Errorf("original request not accessible: %v", err)
		}
	})

	// --- Step 6: User approves, execution reads from latest result ---
	t.Run("approve_revised_option_for_execution", func(t *testing.T) {
		selected := int32(0)
		proposal.Status.Steps.Analysis.SelectedOption = &selected

		result, err := store.GetAnalysisResult(ctx, proposal.Status.Steps.Analysis.Result.Name)
		if err != nil {
			t.Fatalf("GetAnalysisResult for execution: %v", err)
		}

		option := result.Options[*proposal.Status.Steps.Analysis.SelectedOption]
		if option.Title != "Increase to 896MB (compromise)" {
			t.Errorf("selected option title = %q", option.Title)
		}
		if option.Proposal.Risk != "Low" {
			t.Errorf("selected option risk = %q", option.Proposal.Risk)
		}
	})

	// --- Step 7: Proposal CR stays small despite revisions ---
	t.Run("proposal_stays_small_with_revisions", func(t *testing.T) {
		data, err := json.Marshal(proposal)
		if err != nil {
			t.Fatalf("marshal proposal: %v", err)
		}
		t.Logf("proposal JSON size after 2 revisions: %d bytes", len(data))
		if len(data) > 2048 {
			t.Errorf("proposal too large: %d bytes (revisions should not bloat the CR)", len(data))
		}
	})
}

func boolPtr(b bool) *bool                                       { return &b }
func strPtr(s string) *string                                     { return &s }
func int32Ptr(i int32) *int32                                     { return &i }
func sandboxStepPtr(s v1alpha1.SandboxStep) *v1alpha1.SandboxStep { return &s }
