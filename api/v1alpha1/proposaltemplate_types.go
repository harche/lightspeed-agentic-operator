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

// TemplateStep configures one step in the workflow, specifying which
// Agent tier to use.
type TemplateStep struct {
	// agent is the name of the cluster-scoped Agent to use for this step.
	// Defaults to "default" when omitted.
	// +optional
	// +default="default"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Agent string `json:"agent,omitempty"`
}

// ProposalTemplateSpec defines the desired state of ProposalTemplate.
//
// A ProposalTemplate is a reusable workflow shape that defines which
// steps run and which agent tier handles each step. The cluster admin
// creates these on Day 0 as part of operator installation. Component
// teams reference them from Proposals via templateRef.
//
// Steps are skipped by omitting them. Analysis is always required.
type ProposalTemplateSpec struct {
	// analysis defines the analysis step. The analysis agent examines
	// cluster state, produces a diagnosis, remediation proposal,
	// verification plan, and RBAC permissions needed for execution.
	// +required
	Analysis TemplateStep `json:"analysis,omitzero"`

	// execution defines the execution step. When omitted, the proposal
	// transitions to AwaitingSync after approval (advisory/assisted patterns).
	// +optional
	Execution *TemplateStep `json:"execution,omitempty"`

	// verification defines the verification step. When omitted, the
	// proposal completes immediately after execution without verification.
	// +optional
	Verification *TemplateStep `json:"verification,omitempty"`

	// maxAttempts is the default retry limit for proposals using this template.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=20
	MaxAttempts *int32 `json:"maxAttempts,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Analysis",type=string,JSONPath=`.spec.analysis.agent`
// +kubebuilder:printcolumn:name="Execution",type=string,JSONPath=`.spec.execution.agent`
// +kubebuilder:printcolumn:name="Verification",type=string,JSONPath=`.spec.verification.agent`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProposalTemplate defines a reusable workflow shape that controls which
// agent tier handles analysis, execution, and verification. It is
// cluster-scoped and created by the cluster admin.
//
// Example — advisory (analysis only):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: ProposalTemplate
//	metadata:
//	  name: advisory
//	spec:
//	  analysis:
//	    agent: smart
//
// Example — remediation (full pipeline):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: ProposalTemplate
//	metadata:
//	  name: remediation
//	spec:
//	  maxAttempts: 3
//	  analysis:
//	    agent: smart
//	  execution: {}
//	  verification:
//	    agent: fast
//
// Example — assisted (analysis + verification, no execution):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: ProposalTemplate
//	metadata:
//	  name: assisted
//	spec:
//	  analysis:
//	    agent: smart
//	  verification:
//	    agent: fast
type ProposalTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of ProposalTemplate.
	// +required
	Spec ProposalTemplateSpec `json:"spec,omitzero"`
}

// +kubebuilder:object:root=true

// ProposalTemplateList contains a list of ProposalTemplate.
type ProposalTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProposalTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProposalTemplate{}, &ProposalTemplateList{})
}
