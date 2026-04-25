package proposal

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

func TestNeedsRevision(t *testing.T) {
	one := int32(1)
	two := int32(2)
	zero := int32(0)

	tests := []struct {
		name             string
		specRevision     *int32
		observedRevision *int32
		want             bool
	}{
		{"nil_revision", nil, nil, false},
		{"zero_revision", &zero, nil, false},
		{"revision_1_no_observed", &one, nil, true},
		{"revision_1_observed_0", &one, &zero, true},
		{"revision_2_observed_1", &two, &one, true},
		{"revision_1_observed_1", &one, &one, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempt := int32(1)
			proposal := &agenticv1alpha1.Proposal{
				Spec: agenticv1alpha1.ProposalSpec{Revision: tt.specRevision},
				Status: &agenticv1alpha1.ProposalStatus{
					Attempt: &attempt,
					Steps: &agenticv1alpha1.StepsStatus{
						Analysis: &agenticv1alpha1.AnalysisStepStatus{
							ObservedRevision: tt.observedRevision,
						},
					},
				},
			}
			if got := needsRevision(proposal); got != tt.want {
				t.Errorf("needsRevision() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContentReadRBAC(t *testing.T) {
	scheme := testScheme()
	proposal := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{Name: "fix-crash", Namespace: "default"},
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create RBAC
	if err := ensureContentReadRBAC(context.Background(), fc, proposal, "lightspeed-agent", "default"); err != nil {
		t.Fatalf("ensureContentReadRBAC: %v", err)
	}

	// Verify Role exists
	var role rbacv1.Role
	roleName := contentReadRoleName("fix-crash")
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "default"}, &role); err != nil {
		t.Fatalf("Role not found: %v", err)
	}
	if len(role.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(role.Rules))
	}
	if role.Rules[0].APIGroups[0] != "agentic.openshift.io" {
		t.Fatalf("unexpected apiGroup: %s", role.Rules[0].APIGroups[0])
	}
	wantResources := map[string]bool{"analysisresults": true, "requestcontents": true}
	for _, r := range role.Rules[0].Resources {
		if !wantResources[r] {
			t.Fatalf("unexpected resource: %s", r)
		}
	}
	wantVerbs := map[string]bool{"get": true, "list": true}
	for _, v := range role.Rules[0].Verbs {
		if !wantVerbs[v] {
			t.Fatalf("unexpected verb: %s", v)
		}
	}

	// Verify RoleBinding exists
	var binding rbacv1.RoleBinding
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "default"}, &binding); err != nil {
		t.Fatalf("RoleBinding not found: %v", err)
	}
	if binding.Subjects[0].Name != "lightspeed-agent" {
		t.Fatalf("unexpected subject: %s", binding.Subjects[0].Name)
	}

	// Idempotent — second call should not error
	if err := ensureContentReadRBAC(context.Background(), fc, proposal, "lightspeed-agent", "default"); err != nil {
		t.Fatalf("idempotent ensureContentReadRBAC: %v", err)
	}

	// Cleanup
	if err := cleanupContentReadRBAC(context.Background(), fc, proposal); err != nil {
		t.Fatalf("cleanupContentReadRBAC: %v", err)
	}

	// Verify Role deleted
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "default"}, &role); err == nil {
		t.Fatal("Role should be deleted")
	}
	// Verify RoleBinding deleted
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "default"}, &binding); err == nil {
		t.Fatal("RoleBinding should be deleted")
	}
}

func TestReconcile_RevisionContentReadRBACCreated(t *testing.T) {
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

	// Submit revision
	reviseProposal(t, fc, store, "fix-crash", 1, "More memory")

	// Reconcile revision
	reconcileOnce(r, "fix-crash")

	// Verify content-read RBAC was created
	roleName := contentReadRoleName("fix-crash")
	var role rbacv1.Role
	if err := fc.Get(context.Background(), types.NamespacedName{Name: roleName, Namespace: "default"}, &role); err != nil {
		t.Fatalf("content-read Role not created during revision: %v", err)
	}
}
