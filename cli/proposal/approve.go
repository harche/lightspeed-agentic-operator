package proposal

import (
	"context"
	"fmt"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ApproveOptions struct {
	configFlags *genericclioptions.ConfigFlags
	name        string
	option      int32
	wait        bool

	client    client.Client
	namespace string

	genericclioptions.IOStreams
}

func NewApproveCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := &ApproveOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		IOStreams:    streams,
	}

	cmd := &cobra.Command{
		Use:   "approve NAME",
		Short: "Approve a proposal in the Proposed phase",
		Example: `  # Approve a proposal (selects first option by default)
  oc agentic proposal approve fix-crash

  # Approve selecting option 2
  oc agentic proposal approve fix-crash --option=1

  # Approve and wait for completion
  oc agentic proposal approve fix-crash --wait`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}

	o.configFlags.AddFlags(cmd.Flags())
	cmd.Flags().Int32Var(&o.option, "option", 0, "0-based index of the remediation option to select")
	cmd.Flags().BoolVar(&o.wait, "wait", false, "Wait for proposal to reach a terminal phase after approving")

	return cmd
}

func (o *ApproveOptions) Complete(cmd *cobra.Command, args []string) error {
	o.name = args[0]
	var err error
	o.client, err = NewClient(o.configFlags)
	if err != nil {
		return err
	}
	o.namespace, err = ResolveNamespace(o.configFlags)
	return err
}

func (o *ApproveOptions) Validate() error {
	if o.option < 0 {
		return fmt.Errorf("--option must be >= 0")
	}
	return nil
}

func (o *ApproveOptions) Run(ctx context.Context) error {
	p := &agenticv1alpha1.Proposal{}
	if err := o.client.Get(ctx, types.NamespacedName{Name: o.name, Namespace: o.namespace}, p); err != nil {
		return fmt.Errorf("failed to get proposal %q: %w", o.name, err)
	}

	if p.Status.Phase != agenticv1alpha1.ProposalPhaseProposed {
		return fmt.Errorf("cannot approve proposal in phase %s (must be Proposed)", p.Status.Phase)
	}

	numOptions := int32(len(p.Status.Steps.Analysis.Options))
	if numOptions > 0 && o.option >= numOptions {
		return fmt.Errorf("--option %d is out of range (proposal has %d options, valid range: 0-%d)", o.option, numOptions, numOptions-1)
	}

	patch := client.MergeFrom(p.DeepCopy())
	p.Status.Phase = agenticv1alpha1.ProposalPhaseApproved
	p.Status.Steps.Analysis.SelectedOption = &o.option
	if err := o.client.Status().Patch(ctx, p, patch); err != nil {
		return fmt.Errorf("failed to approve proposal: %w", err)
	}

	fmt.Fprintf(o.Out, "proposal/%s approved\n", o.name)

	if o.wait {
		return doWatch(ctx, o.configFlags, o.namespace, o.name, o.Out)
	}
	return nil
}
