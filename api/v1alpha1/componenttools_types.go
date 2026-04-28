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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretRequirement declares a Kubernetes Secret that the sandbox needs
// at runtime. The component owner defines what secrets are needed; the
// cluster admin or customer creates the actual Secret in the same namespace.
type SecretRequirement struct {
	// name of the Secret (must exist in the same namespace as the ComponentTools).
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

// ComponentToolsSpec defines the desired state of ComponentTools.
type ComponentToolsSpec struct {
	// skills defines one or more OCI images containing skills to mount
	// in the agent's sandbox pod. Each entry specifies an image and optionally
	// which paths within that image to mount. The operator creates Kubernetes
	// image volumes (requires K8s 1.34+) and mounts them into the agent's
	// skills directory. Requires 1-20 items.
	//
	// Multiple entries allow composing skills from different images:
	//
	//   skills:
	//     - image: registry.ci.openshift.org/ocp/5.0:agentic-skills
	//       paths:
	//         - /skills/prometheus
	//         - /skills/cluster-update/update-advisor
	//     - image: quay.io/my-org/custom-skills:latest
	//
	// +required
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	Skills []SkillsSource `json:"skills,omitempty"`

	// mcpServers defines external MCP (Model Context Protocol) servers the
	// agent can connect to for additional tools and context beyond its
	// built-in skills. Each server is identified by name and URL.
	// Maximum 20 items.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	MCPServers []MCPServerConfig `json:"mcpServers,omitempty"`

	// systemPrompt is the system prompt text that shapes the agent's
	// behavior for its role (analysis, execution, or verification).
	// When omitted, the agent uses a default prompt appropriate for
	// its workflow step. Maximum 32768 characters.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32768
	SystemPrompt string `json:"systemPrompt,omitempty"`

	// outputSchema is a JSON Schema object that defines additional structured
	// output fields beyond the base schema that every agent produces (diagnosis,
	// proposal, RBAC, verification plan). The operator merges this schema into
	// the base output schema sent to the agent. Use this to request
	// domain-specific structured data from the agent (e.g., an ACS adapter
	// might add a "violationId" string field).
	// +optional
	OutputSchema *apiextensionsv1.JSON `json:"outputSchema,omitempty"`

	// requiredSecrets declares Kubernetes Secrets that the sandbox pod needs
	// at runtime. The component owner defines what secrets are needed; the
	// cluster admin or customer creates the actual Secrets in the same
	// namespace as this ComponentTools resource.
	// Maximum 20 items.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	RequiredSecrets []SecretRequirement `json:"requiredSecrets,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Skills Image",type=string,JSONPath=`.spec.skills[0].image`,priority=1
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ComponentTools defines the domain-specific tools and configuration that a
// component owner (CVO, ACS, CMO, etc.) brings to the agentic platform. It
// is owned by the component team and lives in their namespace alongside any
// required secrets.
//
// ComponentTools is referenced by Workflow steps via componentTools. The
// operator combines it with a cluster-scoped Agent (selected by tier) to
// produce the full sandbox configuration at runtime.
//
// Example — ACS security tools:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: ComponentTools
//	metadata:
//	  name: acs-tools
//	  namespace: stackrox
//	spec:
//	  skills:
//	    - image: quay.io/stackrox/acs-skills:latest
//	  mcpServers:
//	    - name: acs-api
//	      url: https://central.stackrox.svc:8443/v1
//	      headers:
//	        - name: Authorization
//	          valueFrom:
//	            type: Secret
//	            secret:
//	              name: acs-api-token
//	  systemPrompt: |
//	    You are an ACS security remediation agent. Analyze violations
//	    and propose fixes that address the security policy.
//	  requiredSecrets:
//	    - name: acs-api-token
//	      description: "ACS Central API token for querying violations"
//	      mountAs: ACS_API_TOKEN
type ComponentTools struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of ComponentTools.
	// +required
	Spec ComponentToolsSpec `json:"spec,omitzero"`

	// status defines the observed state of ComponentTools.
	// +optional
	// +kubebuilder:validation:MinProperties=1
	Status ComponentToolsStatus `json:"status,omitzero"`
}

const (
	// ComponentToolsConditionReady indicates whether all referenced
	// resources (Secrets, OCI images) are accessible.
	ComponentToolsConditionReady string = "Ready"
)

// ComponentToolsStatus defines the observed state of ComponentTools.
type ComponentToolsStatus struct {
	// conditions represent the latest available observations of the
	// ComponentTools' state. The Ready condition summarizes whether all
	// referenced resources (Secrets, OCI images) are present.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true

// ComponentToolsList contains a list of ComponentTools.
type ComponentToolsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComponentTools `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ComponentTools{}, &ComponentToolsList{})
}
