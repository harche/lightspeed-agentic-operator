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
	"database/sql"
	"fmt"
	"os"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"

	v1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(15432).
		Database("testdb"))

	if err := pg.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start embedded postgres: %v\n", err)
		os.Exit(1)
	}

	var err error
	testDB, err = sql.Open("postgres", "host=localhost port=15432 user=postgres password=postgres dbname=testdb sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect: %v\n", err)
		pg.Stop()
		os.Exit(1)
	}

	code := m.Run()

	testDB.Close()
	pg.Stop()
	os.Exit(code)
}

func newTestStore(t *testing.T) ContentStore {
	t.Helper()
	store, err := NewPostgresContentStore(testDB)
	if err != nil {
		t.Fatalf("NewPostgresContentStore: %v", err)
	}
	for _, table := range []contentTable{tableRequestContent, tableAnalysisResults, tableExecutionResults, tableVerificationResults} {
		if _, err := testDB.Exec(fmt.Sprintf("TRUNCATE %s", table)); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	return store
}

func TestPostgres_RequestContentTextRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	spec := v1alpha1.RequestContentSpec{
		ContentPayload: v1alpha1.ContentPayload{MediaType: "text/plain", Content: "Pod is crashing"},
	}
	if err := store.CreateRequestContent(ctx, "test-request", spec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetRequestContent(ctx, "test-request")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Content != "Pod is crashing" {
		t.Errorf("content = %q", got.Content)
	}
	if got.MediaType != "text/plain" {
		t.Errorf("mediaType = %q", got.MediaType)
	}
}

func TestPostgres_RequestContentBinaryRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	binaryData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
	spec := v1alpha1.RequestContentSpec{
		ContentPayload: v1alpha1.ContentPayload{MediaType: "image/png", Data: binaryData},
	}
	if err := store.CreateRequestContent(ctx, "screenshot", spec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetRequestContent(ctx, "screenshot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MediaType != "image/png" {
		t.Errorf("mediaType = %q", got.MediaType)
	}
	if len(got.Data) != len(binaryData) {
		t.Fatalf("data length = %d, want %d", len(got.Data), len(binaryData))
	}
	for i, b := range got.Data {
		if b != binaryData[i] {
			t.Errorf("data[%d] = %x, want %x", i, b, binaryData[i])
			break
		}
	}
}

func TestPostgres_RequestContentNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.GetRequestContent(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent content")
	}
}

func TestPostgres_RequestContentUpsert(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_ = store.CreateRequestContent(ctx, "req", v1alpha1.RequestContentSpec{ContentPayload: v1alpha1.ContentPayload{Content: "v1"}})
	_ = store.CreateRequestContent(ctx, "req", v1alpha1.RequestContentSpec{ContentPayload: v1alpha1.ContentPayload{Content: "v2"}})

	got, _ := store.GetRequestContent(ctx, "req")
	if got.Content != "v2" {
		t.Errorf("upsert failed: got %q, want v2", got.Content)
	}
}

func TestPostgres_AnalysisResultJSONBRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

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

	if err := store.CreateAnalysisResult(ctx, "test-analysis", spec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetAnalysisResult(ctx, "test-analysis")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if len(got.Options) != 1 {
		t.Fatalf("options = %d, want 1", len(got.Options))
	}
	opt := got.Options[0]
	if opt.Title != "Increase memory limit" {
		t.Errorf("title = %q", opt.Title)
	}
	if opt.Diagnosis.Confidence != "High" {
		t.Errorf("confidence = %q", opt.Diagnosis.Confidence)
	}
	if opt.Proposal.Risk != "Low" {
		t.Errorf("risk = %q", opt.Proposal.Risk)
	}
	if *opt.Proposal.Reversible != true {
		t.Error("reversible lost in JSONB round-trip")
	}
	if opt.RBAC == nil || len(opt.RBAC.NamespaceScoped) != 1 {
		t.Fatal("RBAC lost in JSONB round-trip")
	}
	if opt.RBAC.NamespaceScoped[0].Verbs[1] != "patch" {
		t.Errorf("RBAC verb = %q", opt.RBAC.NamespaceScoped[0].Verbs[1])
	}
	if opt.Verification == nil || len(opt.Verification.Steps) != 1 {
		t.Fatal("verification plan lost in JSONB round-trip")
	}
}

func TestPostgres_ExecutionResultJSONBRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	success := true
	improved := true
	spec := v1alpha1.ExecutionResultSpec{
		ActionsTaken: []v1alpha1.ExecutionAction{
			{Type: "patch", Description: "Patched memory to 512Mi", Success: &success},
		},
		Verification: &v1alpha1.ExecutionVerification{
			ConditionImproved: &improved,
			Summary:           "Pod running stable",
		},
	}

	if err := store.CreateExecutionResult(ctx, "test-exec", spec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetExecutionResult(ctx, "test-exec")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !*got.ActionsTaken[0].Success {
		t.Error("action success lost")
	}
	if !*got.Verification.ConditionImproved {
		t.Error("condition improved lost")
	}
}

func TestPostgres_VerificationResultJSONBRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	passed := true
	spec := v1alpha1.VerificationResultSpec{
		Checks: []v1alpha1.VerifyCheck{
			{Name: "pod-running", Source: "oc", Value: "Running", Passed: &passed},
		},
		Summary: "All checks passed",
	}

	if err := store.CreateVerificationResult(ctx, "test-verify", spec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetVerificationResult(ctx, "test-verify")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !*got.Checks[0].Passed {
		t.Error("check passed lost")
	}
	if got.Summary != "All checks passed" {
		t.Errorf("summary = %q", got.Summary)
	}
}

