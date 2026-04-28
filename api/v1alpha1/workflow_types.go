/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkflowStep defines a single step in the workflow, pairing an Agent tier
// with a ComponentTools configuration.
type WorkflowStep struct {
	// agent is the name of the cluster-scoped Agent (tier) to use for this step.
	// Defaults to "default" when omitted. The cluster admin creates Agent
	// resources (e.g., "default", "smart", "fast"); the component owner
	// references them by name here.
	// +optional
	// +default="default"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Agent string `json:"agent,omitempty"`

	// componentTools references the ComponentTools CR (in the same namespace
	// as the Workflow) that provides skills, MCP servers, system prompt, and
	// output schema for this step.
	// +required
	ComponentTools ComponentToolsReference `json:"componentTools,omitzero"`
}

// WorkflowSpec defines the desired state of Workflow.
//
// A workflow is a 3-step pipeline template. The steps always run in order:
// analysis -> execution -> verification. Between analysis and execution,
// the proposal pauses for user approval (unless the operator is configured
// for auto-approve). Steps are skipped by omitting them.
type WorkflowSpec struct {
	// analysis defines the analysis step configuration. The analysis
	// agent examines the cluster state, produces a diagnosis (root cause,
	// confidence), a remediation proposal (actions, risk, reversibility),
	// a verification plan, and RBAC permissions needed for execution.
	// +required
	Analysis WorkflowStep `json:"analysis,omitzero"`

	// execution defines the execution step configuration. The execution
	// agent carries out the approved remediation plan using the RBAC
	// permissions granted by the operator.
	//
	// When omitted (nil), the proposal transitions to AwaitingSync after
	// approval, making it advisory-only. The user is expected to apply
	// changes manually or via GitOps.
	// +optional
	Execution WorkflowStep `json:"execution,omitzero"`

	// verification defines the verification step configuration. The
	// verification agent checks whether the remediation was successful
	// by running the verification plan produced during analysis.
	//
	// When omitted (nil), the proposal completes immediately after execution
	// without a verification check. Useful for trust-mode workflows where
	// the execution agent's inline verification is sufficient.
	// +optional
	Verification WorkflowStep `json:"verification,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Analysis",type=string,JSONPath=`.spec.analysis.componentTools.name`
// +kubebuilder:printcolumn:name="Execution",type=string,JSONPath=`.spec.execution.componentTools.name`
// +kubebuilder:printcolumn:name="Verification",type=string,JSONPath=`.spec.verification.componentTools.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Workflow defines a reusable 3-step pipeline template that controls which
// agent tier and component tools handle analysis, execution, and verification.
// It is owned by the component team and lives in their namespace alongside
// ComponentTools and Proposals.
//
// Workflow is namespace-scoped. You create workflows representing different
// operational patterns and then reference them from proposals. Per-proposal
// overrides (WorkflowOverride in the Proposal spec) allow swapping agents
// or component tools for individual steps without creating a new Workflow.
//
// Example — full remediation (analyze, execute, verify):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Workflow
//	metadata:
//	  name: remediation
//	spec:
//	  analysis:
//	    agent: smart
//	    componentTools:
//	      name: my-tools
//	  execution:
//	    componentTools:
//	      name: my-tools
//	  verification:
//	    agent: fast
//	    componentTools:
//	      name: my-tools
//
// Example — advisory-only (analyze only, no execution or verification):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Workflow
//	metadata:
//	  name: advisory-only
//	spec:
//	  analysis:
//	    componentTools:
//	      name: my-tools
//
// Example — gitops (analyze, skip execution, verify after user applies via git):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Workflow
//	metadata:
//	  name: gitops-remediation
//	spec:
//	  analysis:
//	    componentTools:
//	      name: my-tools
//	  verification:
//	    componentTools:
//	      name: my-tools
//
// Example — trust-mode (analyze, execute, skip verification):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Workflow
//	metadata:
//	  name: trust-mode
//	spec:
//	  analysis:
//	    componentTools:
//	      name: my-tools
//	  execution:
//	    componentTools:
//	      name: my-tools
type Workflow struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Workflow.
	// +required
	Spec WorkflowSpec `json:"spec,omitzero"`
}

// +kubebuilder:object:root=true

// WorkflowList contains a list of Workflow.
type WorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workflow{}, &WorkflowList{})
}
