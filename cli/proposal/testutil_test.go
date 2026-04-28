package proposal

import (
	"bytes"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = agenticv1alpha1.AddToScheme(s)
	return s
}

func testProposal(name, namespace, template string) *agenticv1alpha1.Proposal {
	return &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Now(),
		},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:          "Pod crashing in production",
			TemplateRef:      &agenticv1alpha1.ProposalTemplateReference{Name: template},
			TargetNamespaces: []string{"production"},
		},
	}
}

func testProposalWithStatus(name, namespace string, phase agenticv1alpha1.ProposalPhase) *agenticv1alpha1.Proposal {
	p := testProposal(name, namespace, "remediation")
	one := int32(1)
	p.Status = agenticv1alpha1.ProposalStatus{
		Phase:   phase,
		Attempt: &one,
	}
	return p
}

func int32Ptr(v int32) *int32 {
	return &v
}

func fakeStreams() (genericclioptions.IOStreams, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	streams := genericclioptions.IOStreams{
		In:     &bytes.Buffer{},
		Out:    out,
		ErrOut: errOut,
	}
	return streams, out, errOut
}
