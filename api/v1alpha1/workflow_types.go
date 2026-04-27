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

// WorkflowSpec defines the desired state of Workflow.
//
// A workflow is a 3-step pipeline template. The steps always run in order:
// analysis -> execution -> verification. Between analysis and execution,
// the proposal pauses for user approval (unless the operator is configured
// for auto-approve). Steps are skipped by omitting them.
type WorkflowSpec struct {
	// analysis references an Agent for the analysis step. The analysis
	// agent examines the cluster state, produces a diagnosis (root cause,
	// confidence), a remediation proposal (actions, risk, reversibility),
	// a verification plan, and RBAC permissions needed for execution.
	// +required
	Analysis AgentReference `json:"analysis,omitempty"`

	// execution references an Agent for the execution step. The execution
	// agent carries out the approved remediation plan using the RBAC
	// permissions granted by the operator.
	//
	// When omitted, the proposal transitions to AwaitingSync after
	// approval, making it advisory-only. The user is expected to apply
	// changes manually or via GitOps.
	// +optional
	Execution *AgentReference `json:"execution,omitempty"`

	// verification references an Agent for the verification step. The
	// verification agent checks whether the remediation was successful
	// by running the verification plan produced during analysis.
	//
	// When omitted, the proposal completes immediately after execution
	// without a verification check. Useful for trust-mode workflows where
	// the execution agent's inline verification is sufficient.
	// +optional
	Verification *AgentReference `json:"verification,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Analysis",type=string,JSONPath=`.spec.analysis.name`
// +kubebuilder:printcolumn:name="Execution",type=string,JSONPath=`.spec.execution.name`
// +kubebuilder:printcolumn:name="Verification",type=string,JSONPath=`.spec.verification.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Workflow defines a reusable 3-step pipeline template that controls which
// agents handle analysis, execution, and verification. It is the third link
// in the CRD chain (LLMProvider -> Agent -> Workflow -> Proposal) and is
// referenced by Proposal resources via spec.workflow.
//
// Workflow is namespace-scoped for multi-tenancy. You create workflows representing different
// operational patterns and then reference them from proposals. Per-proposal
// overrides (WorkflowOverride in the Proposal spec) allow swapping agents
// for individual steps without creating a new Workflow.
//
// Example — full remediation (analyze, execute, verify):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Workflow
//	metadata:
//	  name: remediation
//	spec:
//	  analysis:
//	    name: analyzer
//	  execution:
//	    name: executor
//	  verification:
//	    name: verifier
//
// Example — advisory-only (analyze only, no execution or verification):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Workflow
//	metadata:
//	  name: advisory-only
//	spec:
//	  analysis:
//	    name: analyzer
//
// Example — gitops (analyze, skip execution, verify after user applies via git):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Workflow
//	metadata:
//	  name: gitops-remediation
//	spec:
//	  analysis:
//	    name: analyzer
//	  verification:
//	    name: verifier
//
// Example — trust-mode (analyze, execute, skip verification):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Workflow
//	metadata:
//	  name: trust-mode
//	spec:
//	  analysis:
//	    name: analyzer
//	  execution:
//	    name: executor
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
