package proposal

import (
	"context"
	"strings"
	"testing"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreate_Success(t *testing.T) {
	streams, out, _ := fakeStreams()
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &CreateOptions{
		client:    fc,
		namespace: "default",
		agent:     "default",
		request:   "Pod crashing",
		IOStreams:  streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "created") {
		t.Errorf("expected 'created' in output, got: %s", out.String())
	}
}

func TestCreate_GenerateNamePrefix(t *testing.T) {
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &CreateOptions{
		client:    fc,
		namespace: "default",
		agent:     "default",
		request:   "Pod crashing",
	}

	streams, out, _ := fakeStreams()
	o.IOStreams = streams

	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "proposal/") {
		t.Errorf("expected proposal/ prefix in output, got: %s", output)
	}
}

func TestCreate_WithMaxAttempts(t *testing.T) {
	streams, _, _ := fakeStreams()
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &CreateOptions{
		client:      fc,
		namespace:   "default",
		agent:       "default",
		request:     "Pod crashing",
		maxAttempts: 3,
		IOStreams:    streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	list := &agenticv1alpha1.ProposalList{}
	if err := fc.List(context.Background(), list); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(list.Items))
	}
	if list.Items[0].Spec.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", list.Items[0].Spec.MaxAttempts)
	}
}

func TestCreate_WithoutMaxAttempts(t *testing.T) {
	streams, _, _ := fakeStreams()
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &CreateOptions{
		client:      fc,
		namespace:   "default",
		agent:       "default",
		request:     "Pod crashing",
		maxAttempts: -1,
		IOStreams:    streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	list := &agenticv1alpha1.ProposalList{}
	if err := fc.List(context.Background(), list); err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Items[0].Spec.MaxAttempts != 0 {
		t.Errorf("expected MaxAttempts=0 when not set, got %d", list.Items[0].Spec.MaxAttempts)
	}
}

func TestCreate_WithTargetNamespaces(t *testing.T) {
	streams, _, _ := fakeStreams()
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &CreateOptions{
		client:           fc,
		namespace:        "default",
		agent:            "smart",
		request:          "Pod crashing",
		targetNamespaces: []string{"prod", "staging"},
		maxAttempts:      -1,
		IOStreams:         streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	list := &agenticv1alpha1.ProposalList{}
	if err := fc.List(context.Background(), list); err != nil {
		t.Fatalf("List: %v", err)
	}
	ns := list.Items[0].Spec.TargetNamespaces
	if len(ns) != 2 || ns[0] != "prod" || ns[1] != "staging" {
		t.Errorf("expected target namespaces [prod, staging], got %v", ns)
	}
}

func TestCreate_JSONOutput(t *testing.T) {
	streams, out, _ := fakeStreams()
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &CreateOptions{
		client:      fc,
		namespace:   "default",
		agent:       "smart",
		request:     "Pod crashing",
		output:      "json",
		maxAttempts: -1,
		IOStreams:    streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), `"request"`) || !strings.Contains(out.String(), `"analysis"`) {
		t.Errorf("expected JSON output with proposal fields, got:\n%s", out.String())
	}
}

func TestCreate_Validate(t *testing.T) {
	tests := []struct {
		name    string
		opts    CreateOptions
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty request",
			opts:    CreateOptions{request: "  ", maxAttempts: -1},
			wantErr: true,
			errMsg:  "--request",
		},
		{
			name:    "max-attempts too high",
			opts:    CreateOptions{request: "fix", maxAttempts: 25},
			wantErr: true,
			errMsg:  "--max-attempts",
		},
		{
			name:    "max-attempts negative (not sentinel)",
			opts:    CreateOptions{request: "fix", maxAttempts: -2},
			wantErr: true,
			errMsg:  "--max-attempts",
		},
		{
			name:    "valid",
			opts:    CreateOptions{request: "fix", maxAttempts: -1},
			wantErr: false,
		},
		{
			name:    "max-attempts zero",
			opts:    CreateOptions{request: "fix", maxAttempts: 0},
			wantErr: false,
		},
		{
			name:    "invalid output",
			opts:    CreateOptions{request: "fix", maxAttempts: -1, output: "xml"},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && tc.errMsg != "" && err != nil && !strings.Contains(err.Error(), tc.errMsg) {
				t.Errorf("error should contain %q, got: %v", tc.errMsg, err)
			}
		})
	}
}

func TestCreate_InlineAnalysisAgent(t *testing.T) {
	streams, _, _ := fakeStreams()
	fc := fake.NewClientBuilder().WithScheme(testScheme()).Build()

	o := &CreateOptions{
		client:      fc,
		namespace:   "default",
		agent:       "smart",
		request:     "test",
		maxAttempts: -1,
		IOStreams:    streams,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	list := &agenticv1alpha1.ProposalList{}
	if err := fc.List(context.Background(), list); err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Items[0].Spec.Analysis == nil || list.Items[0].Spec.Analysis.Agent != "smart" {
		t.Errorf("expected analysis agent 'smart', got %v", list.Items[0].Spec.Analysis)
	}
}
