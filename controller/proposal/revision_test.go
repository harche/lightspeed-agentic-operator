package proposal

import (
	"testing"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
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
				Status: agenticv1alpha1.ProposalStatus{
					Attempts: &attempt,
					Steps: agenticv1alpha1.StepsStatus{
						Analysis: agenticv1alpha1.AnalysisStepStatus{
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
