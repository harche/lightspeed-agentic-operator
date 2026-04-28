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

// SecretReference references a Kubernetes Secret by name within the same
// namespace. Used for credentials and authentication tokens.
type SecretReference struct {
	// name of the Secret.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// NamespacedSecretReference references a Kubernetes Secret by name and
// namespace. Used by cluster-scoped resources (LLMProvider) that cannot
// assume same-namespace resolution.
type NamespacedSecretReference struct {
	// name of the Secret.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// namespace of the Secret.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`
}

// LLMProviderReference references a cluster-scoped LLMProvider CR by name.
type LLMProviderReference struct {
	// name of the LLMProvider.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// ComponentToolsReference references a ComponentTools CR by name within the
// same namespace.
type ComponentToolsReference struct {
	// name of the ComponentTools.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// WorkflowReference references a Workflow CR by name within the same namespace.
type WorkflowReference struct {
	// name of the Workflow.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// ProposalReference references a Proposal CR by name within the same
// namespace. Used for escalation parent linkage.
type ProposalReference struct {
	// name of the parent Proposal.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}
