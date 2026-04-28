package proposal

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

type resolvedStep struct {
	Agent *agenticv1alpha1.Agent
	LLM   *agenticv1alpha1.LLMProvider
	Tools *agenticv1alpha1.ToolsSpec
}

type resolvedWorkflow struct {
	Analysis     resolvedStep
	Execution    *resolvedStep // nil = skip execution
	Verification *resolvedStep // nil = skip verification
	MaxAttempts  *int32        // from template, overridden by proposal
}

func resolveProposal(ctx context.Context, c client.Client, proposal *agenticv1alpha1.Proposal) (*resolvedWorkflow, error) {
	agentCache := map[string]*agenticv1alpha1.Agent{}
	llmCache := map[string]*agenticv1alpha1.LLMProvider{}

	resolveAgent := func(agentName string) (*agenticv1alpha1.Agent, *agenticv1alpha1.LLMProvider, error) {
		if agentName == "" {
			agentName = "default"
		}
		agent, ok := agentCache[agentName]
		if !ok {
			agent = &agenticv1alpha1.Agent{}
			if err := c.Get(ctx, types.NamespacedName{Name: agentName}, agent); err != nil {
				return nil, nil, fmt.Errorf("get Agent %q: %w", agentName, err)
			}
			agentCache[agentName] = agent
		}

		llmName := agent.Spec.LLMProvider.Name
		llm, ok := llmCache[llmName]
		if !ok {
			llm = &agenticv1alpha1.LLMProvider{}
			if err := c.Get(ctx, types.NamespacedName{Name: llmName}, llm); err != nil {
				return nil, nil, fmt.Errorf("get LLMProvider %q (referenced by Agent %q): %w", llmName, agentName, err)
			}
			llmCache[llmName] = llm
		}

		return agent, llm, nil
	}

	toolsForStep := func(step *agenticv1alpha1.ProposalStep) *agenticv1alpha1.ToolsSpec {
		if step != nil && step.Tools != nil {
			return step.Tools
		}
		return &proposal.Spec.Tools
	}

	if proposal.Spec.TemplateRef != nil {
		return resolveFromTemplate(ctx, c, proposal, resolveAgent, toolsForStep)
	}
	return resolveInline(proposal, resolveAgent, toolsForStep)
}

type agentResolver func(string) (*agenticv1alpha1.Agent, *agenticv1alpha1.LLMProvider, error)
type toolsResolver func(*agenticv1alpha1.ProposalStep) *agenticv1alpha1.ToolsSpec

func resolveFromTemplate(
	ctx context.Context,
	c client.Client,
	proposal *agenticv1alpha1.Proposal,
	resolveAgent agentResolver,
	toolsForStep toolsResolver,
) (*resolvedWorkflow, error) {
	var tmpl agenticv1alpha1.ProposalTemplate
	if err := c.Get(ctx, types.NamespacedName{Name: proposal.Spec.TemplateRef.Name}, &tmpl); err != nil {
		return nil, fmt.Errorf("get ProposalTemplate %q: %w", proposal.Spec.TemplateRef.Name, err)
	}

	resolved := &resolvedWorkflow{MaxAttempts: tmpl.Spec.MaxAttempts}

	agent, llm, err := resolveAgent(tmpl.Spec.Analysis.Agent)
	if err != nil {
		return nil, fmt.Errorf("resolve analysis step: %w", err)
	}
	resolved.Analysis = resolvedStep{Agent: agent, LLM: llm, Tools: toolsForStep(proposal.Spec.Analysis)}

	if tmpl.Spec.Execution != nil {
		agent, llm, err := resolveAgent(tmpl.Spec.Execution.Agent)
		if err != nil {
			return nil, fmt.Errorf("resolve execution step: %w", err)
		}
		resolved.Execution = &resolvedStep{Agent: agent, LLM: llm, Tools: toolsForStep(proposal.Spec.Execution)}
	}

	if tmpl.Spec.Verification != nil {
		agent, llm, err := resolveAgent(tmpl.Spec.Verification.Agent)
		if err != nil {
			return nil, fmt.Errorf("resolve verification step: %w", err)
		}
		resolved.Verification = &resolvedStep{Agent: agent, LLM: llm, Tools: toolsForStep(proposal.Spec.Verification)}
	}

	return resolved, nil
}

func stepAgentName(step *agenticv1alpha1.ProposalStep) string {
	if step != nil && step.Agent != "" {
		return step.Agent
	}
	return "default"
}

func resolveInline(
	proposal *agenticv1alpha1.Proposal,
	resolveAgent agentResolver,
	toolsForStep toolsResolver,
) (*resolvedWorkflow, error) {
	resolved := &resolvedWorkflow{}

	agent, llm, err := resolveAgent(stepAgentName(proposal.Spec.Analysis))
	if err != nil {
		return nil, fmt.Errorf("resolve analysis step: %w", err)
	}
	resolved.Analysis = resolvedStep{Agent: agent, LLM: llm, Tools: toolsForStep(proposal.Spec.Analysis)}

	if proposal.Spec.Execution != nil {
		agent, llm, err := resolveAgent(stepAgentName(proposal.Spec.Execution))
		if err != nil {
			return nil, fmt.Errorf("resolve execution step: %w", err)
		}
		resolved.Execution = &resolvedStep{Agent: agent, LLM: llm, Tools: toolsForStep(proposal.Spec.Execution)}
	}

	if proposal.Spec.Verification != nil {
		agent, llm, err := resolveAgent(stepAgentName(proposal.Spec.Verification))
		if err != nil {
			return nil, fmt.Errorf("resolve verification step: %w", err)
		}
		resolved.Verification = &resolvedStep{Agent: agent, LLM: llm, Tools: toolsForStep(proposal.Spec.Verification)}
	}

	return resolved, nil
}
