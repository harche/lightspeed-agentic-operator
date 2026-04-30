package proposal

import (
	"context"
	"fmt"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DenyOptions struct {
	configFlags *genericclioptions.ConfigFlags
	name        string

	client    client.Client
	namespace string

	genericclioptions.IOStreams
}

func NewDenyCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := &DenyOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		IOStreams:    streams,
	}

	cmd := &cobra.Command{
		Use:   "deny NAME",
		Short: "Deny a proposal in the Proposed phase",
		Example: `  # Deny a proposal
  oc agentic proposal deny fix-crash`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd, args); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func (o *DenyOptions) Complete(cmd *cobra.Command, args []string) error {
	o.name = args[0]
	var err error
	o.client, err = NewClient(o.configFlags)
	if err != nil {
		return err
	}
	o.namespace, err = ResolveNamespace(o.configFlags)
	return err
}

func (o *DenyOptions) Run(ctx context.Context) error {
	p := &agenticv1alpha1.Proposal{}
	if err := o.client.Get(ctx, types.NamespacedName{Name: o.name, Namespace: o.namespace}, p); err != nil {
		return fmt.Errorf("failed to get proposal %q: %w", o.name, err)
	}

	phase := agenticv1alpha1.DerivePhase(p.Status.Conditions)
	if phase != agenticv1alpha1.ProposalPhaseProposed {
		return fmt.Errorf("cannot deny proposal in phase %s (must be Proposed)", phase)
	}

	patch := client.MergeFrom(p.DeepCopy())
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:    agenticv1alpha1.ProposalConditionApproved,
		Status:  metav1.ConditionFalse,
		Reason:  "UserDenied",
		Message: "Proposal denied by user via CLI",
	})
	if err := o.client.Status().Patch(ctx, p, patch); err != nil {
		return fmt.Errorf("failed to deny proposal: %w", err)
	}

	fmt.Fprintf(o.Out, "proposal/%s denied\n", o.name)
	return nil
}
