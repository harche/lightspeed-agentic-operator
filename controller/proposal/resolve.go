package proposal

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

type resolvedStep struct {
	Agent          *agenticv1alpha1.Agent
	LLM            *agenticv1alpha1.LLMProvider
	ComponentTools *agenticv1alpha1.ComponentTools
}

type resolvedWorkflow struct {
	Analysis     resolvedStep
	Execution    *resolvedStep // nil = skip execution
	Verification *resolvedStep // nil = skip verification
}

func effectiveStepConfig(step agenticv1alpha1.WorkflowStep, override agenticv1alpha1.WorkflowStepOverride) (agentName, ctName string) {
	agentName = step.Agent
	if override.Agent != "" {
		agentName = override.Agent
	}
	ctName = step.ComponentTools.Name
	if override.ComponentTools.Name != "" {
		ctName = override.ComponentTools.Name
	}
	return agentName, ctName
}

// resolveWorkflow fetches the Workflow CR, applies per-proposal overrides,
// and resolves each step's Agent + LLMProvider + ComponentTools.
func resolveWorkflow(ctx context.Context, c client.Client, proposal *agenticv1alpha1.Proposal) (*resolvedWorkflow, error) {
	ns := proposal.Namespace

	var wf agenticv1alpha1.Workflow
	if err := c.Get(ctx, types.NamespacedName{Name: proposal.Spec.Workflow.Name, Namespace: ns}, &wf); err != nil {
		return nil, fmt.Errorf("get Workflow %q: %w", proposal.Spec.Workflow.Name, err)
	}

	agentCache := map[string]*agenticv1alpha1.Agent{}
	llmCache := map[string]*agenticv1alpha1.LLMProvider{}

	resolveStepCached := func(agentName, componentToolsName string) (*resolvedStep, error) {
		agent, ok := agentCache[agentName]
		if !ok {
			agent = &agenticv1alpha1.Agent{}
			if err := c.Get(ctx, types.NamespacedName{Name: agentName}, agent); err != nil {
				return nil, fmt.Errorf("get Agent %q: %w", agentName, err)
			}
			agentCache[agentName] = agent
		}

		llmName := agent.Spec.LLMProvider.Name
		llm, ok := llmCache[llmName]
		if !ok {
			llm = &agenticv1alpha1.LLMProvider{}
			if err := c.Get(ctx, types.NamespacedName{Name: llmName}, llm); err != nil {
				return nil, fmt.Errorf("get LLMProvider %q (referenced by Agent %q): %w", llmName, agentName, err)
			}
			llmCache[llmName] = llm
		}

		var ct agenticv1alpha1.ComponentTools
		if err := c.Get(ctx, types.NamespacedName{Name: componentToolsName, Namespace: ns}, &ct); err != nil {
			return nil, fmt.Errorf("get ComponentTools %q: %w", componentToolsName, err)
		}

		return &resolvedStep{Agent: agent, LLM: llm, ComponentTools: &ct}, nil
	}

	resolved := &resolvedWorkflow{}

	agent, ct := effectiveStepConfig(wf.Spec.Analysis, proposal.Spec.WorkflowOverride.Analysis)
	step, err := resolveStepCached(agent, ct)
	if err != nil {
		return nil, fmt.Errorf("resolve analysis step: %w", err)
	}
	resolved.Analysis = *step

	if wf.Spec.Execution != (agenticv1alpha1.WorkflowStep{}) {
		agent, ct := effectiveStepConfig(wf.Spec.Execution, proposal.Spec.WorkflowOverride.Execution)
		step, err := resolveStepCached(agent, ct)
		if err != nil {
			return nil, fmt.Errorf("resolve execution step: %w", err)
		}
		resolved.Execution = step
	}

	if wf.Spec.Verification != (agenticv1alpha1.WorkflowStep{}) {
		agent, ct := effectiveStepConfig(wf.Spec.Verification, proposal.Spec.WorkflowOverride.Verification)
		step, err := resolveStepCached(agent, ct)
		if err != nil {
			return nil, fmt.Errorf("resolve verification step: %w", err)
		}
		resolved.Verification = step
	}

	return resolved, nil
}
