package proposal

import (
	"testing"

	"github.com/go-logr/logr"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

func ptr32(v int32) *int32 { return &v }

func makeOptions(titles ...string) []agenticv1alpha1.RemediationOption {
	opts := make([]agenticv1alpha1.RemediationOption, len(titles))
	for i, t := range titles {
		opts[i] = agenticv1alpha1.RemediationOption{Title: t}
	}
	return opts
}

func TestPruneToSelectedOption(t *testing.T) {
	tests := []struct {
		name           string
		options        []agenticv1alpha1.RemediationOption
		selected       *int32
		wantCount      int
		wantSelected   *int32
		wantFirstTitle string
	}{
		{
			name:      "nil_selected",
			options:   makeOptions("A", "B", "C"),
			selected:  nil,
			wantCount: 3, wantSelected: nil,
		},
		{
			name:      "empty_options",
			options:   nil,
			selected:  ptr32(0),
			wantCount: 0, wantSelected: ptr32(0),
		},
		{
			name:      "single_option_selected_0",
			options:   makeOptions("A"),
			selected:  ptr32(0),
			wantCount: 1, wantSelected: ptr32(0), wantFirstTitle: "A",
		},
		{
			name:      "three_options_selected_0",
			options:   makeOptions("A", "B", "C"),
			selected:  ptr32(0),
			wantCount: 1, wantSelected: ptr32(0), wantFirstTitle: "A",
		},
		{
			name:      "three_options_selected_1",
			options:   makeOptions("A", "B", "C"),
			selected:  ptr32(1),
			wantCount: 1, wantSelected: ptr32(0), wantFirstTitle: "B",
		},
		{
			name:      "three_options_selected_2",
			options:   makeOptions("A", "B", "C"),
			selected:  ptr32(2),
			wantCount: 1, wantSelected: ptr32(0), wantFirstTitle: "C",
		},
		{
			name:      "ten_options_selected_5",
			options:   makeOptions("0", "1", "2", "3", "4", "5", "6", "7", "8", "9"),
			selected:  ptr32(5),
			wantCount: 1, wantSelected: ptr32(0), wantFirstTitle: "5",
		},
		{
			name:      "out_of_range",
			options:   makeOptions("A", "B", "C"),
			selected:  ptr32(5),
			wantCount: 3, wantSelected: ptr32(5),
		},
		{
			name:      "negative_index",
			options:   makeOptions("A", "B", "C"),
			selected:  ptr32(-1),
			wantCount: 3, wantSelected: ptr32(-1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := &agenticv1alpha1.AnalysisStepStatus{
				Options:        tt.options,
				SelectedOption: tt.selected,
			}
			pruneToSelectedOption(analysis)

			if got := len(analysis.Options); got != tt.wantCount {
				t.Errorf("Options count = %d, want %d", got, tt.wantCount)
			}
			if tt.wantSelected == nil {
				if analysis.SelectedOption != nil {
					t.Errorf("SelectedOption = %d, want nil", *analysis.SelectedOption)
				}
			} else {
				if analysis.SelectedOption == nil {
					t.Fatalf("SelectedOption = nil, want %d", *tt.wantSelected)
				}
				if *analysis.SelectedOption != *tt.wantSelected {
					t.Errorf("SelectedOption = %d, want %d", *analysis.SelectedOption, *tt.wantSelected)
				}
			}
			if tt.wantFirstTitle != "" && analysis.Options[0].Title != tt.wantFirstTitle {
				t.Errorf("Options[0].Title = %q, want %q", analysis.Options[0].Title, tt.wantFirstTitle)
			}
		})
	}
}

func TestSelectedOption_AfterPrune(t *testing.T) {
	proposal := &agenticv1alpha1.Proposal{}
	proposal.Name = "test"
	proposal.Status.Steps.Analysis.Options = makeOptions("A", "B", "C")
	proposal.Status.Steps.Analysis.SelectedOption = ptr32(2)

	pruneToSelectedOption(&proposal.Status.Steps.Analysis)

	r := &ProposalReconciler{Log: logr.Discard()}
	got := r.selectedOption(proposal)
	if got == nil {
		t.Fatal("selectedOption() returned nil after prune")
	}
	if got.Title != "C" {
		t.Errorf("selectedOption().Title = %q, want %q", got.Title, "C")
	}
}
