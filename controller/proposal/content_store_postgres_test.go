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

func TestPostgres_RequestContentRoundTrip(t *testing.T) {
	binaryData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

	tests := []struct {
		name      string
		spec      v1alpha1.RequestContentSpec
		checkText string
		checkData []byte
	}{
		{
			name: "text",
			spec: v1alpha1.RequestContentSpec{
				ContentPayload: v1alpha1.ContentPayload{MediaType: "text/plain", Content: "Pod is crashing"},
			},
			checkText: "Pod is crashing",
		},
		{
			name: "binary",
			spec: v1alpha1.RequestContentSpec{
				ContentPayload: v1alpha1.ContentPayload{MediaType: "image/png", Data: binaryData},
			},
			checkData: binaryData,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			name := "test-" + tt.name

			if err := store.CreateRequestContent(ctx, name, tt.spec); err != nil {
				t.Fatalf("create: %v", err)
			}
			got, err := store.GetRequestContent(ctx, name)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.MediaType != tt.spec.MediaType {
				t.Errorf("mediaType = %q, want %q", got.MediaType, tt.spec.MediaType)
			}
			if tt.checkText != "" && got.Content != tt.checkText {
				t.Errorf("content = %q, want %q", got.Content, tt.checkText)
			}
			if tt.checkData != nil {
				if len(got.Data) != len(tt.checkData) {
					t.Fatalf("data length = %d, want %d", len(got.Data), len(tt.checkData))
				}
				for i, b := range got.Data {
					if b != tt.checkData[i] {
						t.Errorf("data[%d] = %x, want %x", i, b, tt.checkData[i])
						break
					}
				}
			}
		})
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

func TestPostgres_ResultJSONBRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		create func(ContentStore, context.Context) error
		verify func(ContentStore, context.Context, *testing.T)
	}{
		{
			name: "analysis",
			create: func(s ContentStore, ctx context.Context) error {
				return s.CreateAnalysisResult(ctx, "test-analysis", v1alpha1.AnalysisResultSpec{
					Options: []v1alpha1.RemediationOption{{
						Title: "Increase memory limit",
						Diagnosis: v1alpha1.DiagnosisResult{
							Summary: "OOMKilled due to 256Mi limit", Confidence: "High", RootCause: "Memory limit too low",
						},
						Proposal: v1alpha1.ProposalResult{
							Description: "Increase memory from 256Mi to 512Mi",
							Actions:     []v1alpha1.ProposedAction{{Type: "patch", Description: "Patch deployment"}},
							Risk:        "Low",
							Reversible:  boolPtr(true),
						},
						RBAC: &v1alpha1.RBACResult{
							NamespaceScoped: []v1alpha1.RBACRule{{
								APIGroups: []string{"apps"}, Resources: []string{"deployments"},
								Verbs: []string{"get", "patch"}, Justification: "Patch deployment memory limit",
							}},
						},
						Verification: &v1alpha1.VerificationPlan{
							Description: "Verify pod is running",
							Steps:       []v1alpha1.VerificationStep{{Name: "pod-running", Command: "oc get pod", Expected: "Running", Type: "command"}},
						},
					}},
				})
			},
			verify: func(s ContentStore, ctx context.Context, t *testing.T) {
				got, err := s.GetAnalysisResult(ctx, "test-analysis")
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
				if *opt.Proposal.Reversible != true {
					t.Error("reversible lost")
				}
				if opt.RBAC == nil || len(opt.RBAC.NamespaceScoped) != 1 {
					t.Fatal("RBAC lost")
				}
				if opt.RBAC.NamespaceScoped[0].Verbs[1] != "patch" {
					t.Errorf("RBAC verb = %q", opt.RBAC.NamespaceScoped[0].Verbs[1])
				}
				if opt.Verification == nil || len(opt.Verification.Steps) != 1 {
					t.Fatal("verification plan lost")
				}
			},
		},
		{
			name: "execution",
			create: func(s ContentStore, ctx context.Context) error {
				return s.CreateExecutionResult(ctx, "test-exec", v1alpha1.ExecutionResultSpec{
					ActionsTaken: []v1alpha1.ExecutionAction{
						{Type: "patch", Description: "Patched memory to 512Mi", Success: boolPtr(true)},
					},
					Verification: &v1alpha1.ExecutionVerification{
						ConditionImproved: boolPtr(true), Summary: "Pod running stable",
					},
				})
			},
			verify: func(s ContentStore, ctx context.Context, t *testing.T) {
				got, err := s.GetExecutionResult(ctx, "test-exec")
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if !*got.ActionsTaken[0].Success {
					t.Error("action success lost")
				}
				if !*got.Verification.ConditionImproved {
					t.Error("condition improved lost")
				}
			},
		},
		{
			name: "verification",
			create: func(s ContentStore, ctx context.Context) error {
				return s.CreateVerificationResult(ctx, "test-verify", v1alpha1.VerificationResultSpec{
					Checks:  []v1alpha1.VerifyCheck{{Name: "pod-running", Source: "oc", Value: "Running", Passed: boolPtr(true)}},
					Summary: "All checks passed",
				})
			},
			verify: func(s ContentStore, ctx context.Context, t *testing.T) {
				got, err := s.GetVerificationResult(ctx, "test-verify")
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if !*got.Checks[0].Passed {
					t.Error("check passed lost")
				}
				if got.Summary != "All checks passed" {
					t.Errorf("summary = %q", got.Summary)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()

			if err := tt.create(store, ctx); err != nil {
				t.Fatalf("create: %v", err)
			}
			tt.verify(store, ctx, t)
		})
	}
}
