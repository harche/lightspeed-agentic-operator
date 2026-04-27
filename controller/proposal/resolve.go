package proposal

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agenticv1alpha1 "github.com/harche/lightspeed-agentic-operator/api/v1alpha1"
)

type resolvedStep struct {
	Agent *agenticv1alpha1.Agent
	LLM   *agenticv1alpha1.LLMProvider
}

type resolvedWorkflow struct {
	Analysis     resolvedStep
	Execution    *resolvedStep // nil = skip execution
	Verification *resolvedStep // nil = skip verification
}

// resolveWorkflow fetches the Workflow CR, applies per-proposal overrides,
// and resolves each step's Agent + LLMProvider.
func resolveWorkflow(ctx context.Context, c client.Client, proposal *agenticv1alpha1.Proposal) (*resolvedWorkflow, error) {
	ns := proposal.Namespace

	var wf agenticv1alpha1.Workflow
	if err := c.Get(ctx, types.NamespacedName{Name: proposal.Spec.Workflow.Name, Namespace: ns}, &wf); err != nil {
		return nil, fmt.Errorf("get Workflow %q: %w", proposal.Spec.Workflow.Name, err)
	}

	// Determine effective agent names, applying per-proposal overrides.
	analysisAgentName := wf.Spec.Analysis.Name
	if o := proposal.Spec.WorkflowOverride; o != nil && o.Analysis != nil {
		analysisAgentName = o.Analysis.Name
	}

	var executionAgentName string
	if wf.Spec.Execution != nil {
		executionAgentName = wf.Spec.Execution.Name
	}
	if o := proposal.Spec.WorkflowOverride; o != nil && o.Execution != nil {
		executionAgentName = o.Execution.Name
	}

	var verificationAgentName string
	if wf.Spec.Verification != nil {
		verificationAgentName = wf.Spec.Verification.Name
	}
	if o := proposal.Spec.WorkflowOverride; o != nil && o.Verification != nil {
		verificationAgentName = o.Verification.Name
	}

	resolved := &resolvedWorkflow{}

	// Analysis is always required.
	step, err := resolveStep(ctx, c, ns, analysisAgentName)
	if err != nil {
		return nil, fmt.Errorf("resolve analysis step: %w", err)
	}
	resolved.Analysis = *step

	if executionAgentName != "" {
		step, err := resolveStep(ctx, c, ns, executionAgentName)
		if err != nil {
			return nil, fmt.Errorf("resolve execution step: %w", err)
		}
		resolved.Execution = step
	}

	if verificationAgentName != "" {
		step, err := resolveStep(ctx, c, ns, verificationAgentName)
		if err != nil {
			return nil, fmt.Errorf("resolve verification step: %w", err)
		}
		resolved.Verification = step
	}

	return resolved, nil
}

func resolveStep(ctx context.Context, c client.Client, ns, agentName string) (*resolvedStep, error) {
	var agent agenticv1alpha1.Agent
	if err := c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent); err != nil {
		return nil, fmt.Errorf("get Agent %q: %w", agentName, err)
	}

	var llm agenticv1alpha1.LLMProvider
	if err := c.Get(ctx, types.NamespacedName{Name: agent.Spec.LLMProvider.Name, Namespace: ns}, &llm); err != nil {
		return nil, fmt.Errorf("get LLMProvider %q (referenced by Agent %q): %w", agent.Spec.LLMProvider.Name, agentName, err)
	}

	return &resolvedStep{Agent: &agent, LLM: &llm}, nil
}
