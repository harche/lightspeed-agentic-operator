package proposal

import (
	"context"
	"strings"
	"testing"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDeny_Success(t *testing.T) {
	streams, out, _ := fakeStreams()
	p := testProposalWithStatus("fix-crash", "default", agenticv1alpha1.ProposalPhaseProposed)

	fc := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(p).WithStatusSubresource(p).Build()

	o := &DenyOptions{
		client:    fc,
		name:      "fix-crash",
		namespace: "default",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "denied") {
		t.Errorf("expected 'denied' in output, got: %s", out.String())
	}

	var updated agenticv1alpha1.Proposal
	if err := fc.Get(context.Background(), types.NamespacedName{Name: "fix-crash", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agenticv1alpha1.DerivePhase(updated.Status.Conditions) != agenticv1alpha1.ProposalPhaseDenied {
		t.Errorf("expected Denied phase, got %s", agenticv1alpha1.DerivePhase(updated.Status.Conditions))
	}
}

func TestDeny_WrongPhase(t *testing.T) {
	phases := []agenticv1alpha1.ProposalPhase{
		agenticv1alpha1.ProposalPhasePending,
		agenticv1alpha1.ProposalPhaseAnalyzing,
		agenticv1alpha1.ProposalPhaseApproved,
		agenticv1alpha1.ProposalPhaseExecuting,
		agenticv1alpha1.ProposalPhaseCompleted,
		agenticv1alpha1.ProposalPhaseFailed,
	}
	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			streams, _, _ := fakeStreams()
			p := testProposalWithStatus("fix-crash", "default", phase)

			fc := fake.NewClientBuilder().WithScheme(testScheme()).
				WithObjects(p).WithStatusSubresource(p).Build()

			o := &DenyOptions{
				client:    fc,
				name:      "fix-crash",
				namespace: "default",
				IOStreams:  streams,
			}
			err := o.Run(context.Background())
			if err == nil {
				t.Fatal("expected error for non-Proposed phase")
			}
			if !strings.Contains(err.Error(), "must be Proposed") {
				t.Errorf("error should mention 'must be Proposed', got: %v", err)
			}
		})
	}
}

func TestDeny_NotFound(t *testing.T) {
	streams, _, _ := fakeStreams()
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &DenyOptions{
		client:    fc,
		name:      "nonexistent",
		namespace: "default",
		IOStreams:  streams,
	}
	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for nonexistent proposal")
	}
}
