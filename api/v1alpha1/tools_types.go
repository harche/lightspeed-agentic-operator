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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// SecretRequirement declares a Kubernetes Secret that the sandbox needs
// at runtime. The cluster admin creates the actual Secret in the same
// namespace as the Proposal.
type SecretRequirement struct {
	// name of the Secret (must exist in the same namespace as the Proposal).
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// description explains what this secret is used for, helping the
	// cluster admin understand what credentials to provide.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	Description string `json:"description,omitempty"`

	// mountAs specifies how the secret is exposed in the sandbox pod.
	// Use an environment variable name (e.g., "GITHUB_TOKEN") to inject
	// as an env var, or a file path (e.g., "/etc/secrets/token") to
	// mount as a file.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	MountAs string `json:"mountAs,omitempty"`
}

// ToolsSpec defines the tools available to an agent in its sandbox pod.
// This includes skills images, required secrets, and an optional output
// schema for structured agent output.
//
// ToolsSpec is specified on a Proposal either as a shared default
// (spec.tools) or per-step (spec.analysis.tools, spec.execution.tools,
// spec.verification.tools). Per-step tools replace the shared default
// for that step.
type ToolsSpec struct {
	// skills defines one or more OCI images containing skills to mount
	// in the agent's sandbox pod. The operator creates Kubernetes image
	// volumes (requires K8s 1.34+) and mounts them into the agent's
	// skills directory.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	Skills []SkillsSource `json:"skills,omitempty"`

	// mcpServers defines external MCP (Model Context Protocol) servers the
	// agent can connect to for additional tools and context.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	MCPServers []MCPServerConfig `json:"mcpServers,omitempty"`

	// requiredSecrets declares Kubernetes Secrets that the sandbox pod
	// needs at runtime. The cluster admin creates the actual Secrets
	// in the same namespace as the Proposal.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	RequiredSecrets []SecretRequirement `json:"requiredSecrets,omitempty"`

	// outputSchema is a JSON Schema object that defines additional structured
	// output fields beyond the base schema that every agent produces (diagnosis,
	// proposal, RBAC, verification plan).
	// +optional
	OutputSchema *apiextensionsv1.JSON `json:"outputSchema,omitempty"`
}
