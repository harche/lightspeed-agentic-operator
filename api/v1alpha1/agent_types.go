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
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCPHeaderSourceType defines how a header value is sourced when the
// operator configures MCP server connections for an agent.
//
//   - "Secret"     — The value is read from a Kubernetes Secret referenced
//     by secretRef. Use this for API keys and tokens.
//   - "Kubernetes" — The operator injects a Kubernetes service account
//     token automatically (for MCP servers that accept K8s auth).
//   - "Client"     — The value is provided by the calling client at
//     runtime (e.g., forwarded from a user session).
//
// +kubebuilder:validation:Enum=Secret;Kubernetes;Client
type MCPHeaderSourceType string

const (
	// MCPHeaderSourceTypeSecret reads the header value from a Kubernetes Secret.
	MCPHeaderSourceTypeSecret MCPHeaderSourceType = "Secret"
	// MCPHeaderSourceTypeKubernetes uses an auto-injected Kubernetes SA token.
	MCPHeaderSourceTypeKubernetes MCPHeaderSourceType = "Kubernetes"
	// MCPHeaderSourceTypeClient expects the value to be provided by the caller.
	MCPHeaderSourceTypeClient MCPHeaderSourceType = "Client"
)

// MCPHeaderValueSource defines where to obtain the value for an MCP header.
// Exactly one source is used depending on the type field.
// +kubebuilder:validation:XValidation:rule="self.type == 'Secret' ? has(self.secretRef) && size(self.secretRef.name) > 0 : true",message="secretRef with non-empty name is required when type is 'Secret'"
// +kubebuilder:validation:XValidation:rule="self.type != 'Secret' ? !has(self.secretRef) : true",message="secretRef must not be set when type is 'Kubernetes' or 'Client'"
type MCPHeaderValueSource struct {
	// type specifies the source type for the header value.
	// +required
	Type MCPHeaderSourceType `json:"type,omitempty"`

	// secretRef references a secret containing the header value.
	// Required when type is "secret".
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// MCPHeader defines an HTTP header to send with every request to an
// MCP server. Used for authentication and routing.
type MCPHeader struct {
	// name of the header (e.g., "Authorization", "X-API-Key").
	// Must be at least 1 character, containing only letters, digits, and hyphens.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9-]+$`
	Name string `json:"name,omitempty"`

	// valueFrom is the source of the header value.
	// +required
	ValueFrom MCPHeaderValueSource `json:"valueFrom,omitzero"`
}

// MCPServerConfig defines the configuration for an MCP (Model Context Protocol)
// server that the agent can connect to for additional tools and context.
// MCP servers extend the agent's capabilities beyond its built-in skills.
//
// Example — connecting to an OpenShift MCP server with SA token auth:
//
//	mcpServers:
//	  - name: openshift
//	    url: https://mcp.openshift-lightspeed.svc:8443/sse
//	    timeoutSeconds: 10
//	    headers:
//	      - name: Authorization
//	        valueFrom:
//	          type: Kubernetes
//
// Example — connecting to an external API with secret-based auth:
//
//	mcpServers:
//	  - name: pagerduty
//	    url: https://mcp-pagerduty.example.com/sse
//	    headers:
//	      - name: X-API-Key
//	        valueFrom:
//	          type: Secret
//	          secretRef:
//	            name: pagerduty-api-key
type MCPServerConfig struct {
	// name of the MCP server. Must be 1-253 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// url of the MCP server (HTTP/HTTPS). Must be an HTTP or HTTPS URL,
	// maximum 2048 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Pattern=`^https?://.*$`
	URL string `json:"url,omitempty"`

	// timeoutSeconds is the timeout for the MCP server in seconds, default is 5.
	// Valid range: 1-300.
	// +optional
	// +default=5
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=300
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`

	// headers to send to the MCP server. Maximum 20 items.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=20
	Headers []MCPHeader `json:"headers,omitempty"`
}

// SkillsSource defines an OCI image containing skills and optionally which
// paths within that image to mount. Skills are mounted as Kubernetes image
// volumes in the agent's sandbox pod.
//
// When paths is omitted, the entire image is mounted. When paths is specified,
// only those directories are mounted (each as a separate subPath volumeMount),
// allowing selective composition of skills from large shared images.
//
// Example — mount all skills from a custom image:
//
//	skills:
//	  - image: quay.io/my-org/my-skills:latest
//
// Example — selectively mount two skills from a shared image:
//
//	skills:
//	  - image: registry.ci.openshift.org/ocp/5.0:agentic-skills
//	    paths:
//	      - /skills/prometheus
//	      - /skills/cluster-update/update-advisor
type SkillsSource struct {
	// image is the OCI image reference containing skills.
	// The operator mounts this as a Kubernetes image volume (requires K8s 1.34+).
	// Must be 1-512 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Image string `json:"image,omitempty"`

	// paths restricts which directories from the image are mounted.
	// Each path is mounted as a separate subPath volumeMount into the agent's
	// skills directory. The last segment of each path becomes the mount name
	// (e.g., "/skills/prometheus" mounts as "prometheus").
	//
	// When omitted, the entire image is mounted as a single volume.
	// Maximum 50 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=50
	Paths []string `json:"paths,omitempty"`
}

// ContentReference points to content served by the platform's aggregated
// content API. The storage backend (etcd, object storage, PVC) is
// configured by the cluster admin independently. API consumers reference
// content by name without knowledge of where it is physically stored.
// The operator resolves these references at reconcile time via the
// aggregated API server.
type ContentReference struct {
	// name of the content resource.
	// Must be 1-253 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// AgentSpec defines the desired state of Agent.
// +kubebuilder:validation:XValidation:rule="self.llmRef.name != ''",message="llmRef.name must not be empty"
type AgentSpec struct {
	// image optionally overrides the agent container image used in the
	// sandbox pod. When omitted, the operator uses the default agent image
	// from the base SandboxTemplate.
	//
	// The operator communicates with the agent exclusively via HTTP POST
	// to /analyze, /execute, and /verify on port 8080. Custom images must
	// serve these endpoints and return the expected JSON response format.
	// The simplest way to satisfy this contract is to base your image on
	// the default lightspeed-service image and add your own tools on top.
	// Must be 1-512 characters when set.
	//
	// Example:
	//
	//	image: quay.io/my-org/my-agent:v2
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Image *string `json:"image,omitempty"`

	// llmRef references an LLMProvider CR that supplies the
	// LLM backend for this agent. The operator resolves this reference at
	// reconcile time and configures the sandbox pod with the provider's
	// credentials and model.
	// +required
	LLMRef corev1.LocalObjectReference `json:"llmRef,omitempty"`

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
	// +kubebuilder:validation:MaxItems=20
	MCPServers []MCPServerConfig `json:"mcpServers,omitempty"`

	// systemPrompt is the system prompt text that shapes the agent's
	// behavior for its role (analysis, execution, or verification).
	// When omitted, the agent uses a default prompt appropriate for
	// its workflow step. Maximum 32768 characters.
	// +optional
	// +kubebuilder:validation:MaxLength=32768
	SystemPrompt *string `json:"systemPrompt,omitempty"`

	// outputSchema is a JSON Schema object that defines additional structured
	// output fields beyond the base schema that every agent produces (diagnosis,
	// proposal, RBAC, verification plan). The operator merges this schema into
	// the base output schema sent to the agent. Use this to request
	// domain-specific structured data from the agent (e.g., an ACS adapter
	// might add a "violationId" string field).
	// +optional
	OutputSchema *apiextensionsv1.JSON `json:"outputSchema,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="LLM",type=string,JSONPath=`.spec.llmRef.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Skills Image",type=string,JSONPath=`.spec.skills[0].image`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Agent defines a complete agent configuration: which LLM to use, what
// skills to mount, optional MCP servers, and what system prompt to follow.
// It is the second link in the CRD chain (LLMProvider -> Agent -> Workflow
// -> Proposal) and is referenced by Workflow steps via agentRef.
//
// Agent is namespace-scoped for multi-tenancy. You typically create a few agents with different
// capabilities and assign them to workflow steps. For example, an analysis
// agent might use a capable model with broad diagnostic skills, while an
// execution agent uses a fast model with targeted remediation skills.
//
// Example — an analysis agent with selective skills and a system prompt:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Agent
//	metadata:
//	  name: analyzer
//	spec:
//	  llmRef:
//	    name: smart
//	  skills:
//	    - image: registry.ci.openshift.org/ocp/5.0:agentic-skills
//	      paths:
//	        - /skills/prometheus
//	        - /skills/cluster-ops
//	        - /skills/rbac-security
//	  systemPrompt: |
//	    You are an SRE analyst. Examine cluster state, identify root
//	    causes, and propose remediation options with RBAC requirements.
//
// Example — an execution agent with a fast model:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Agent
//	metadata:
//	  name: executor
//	spec:
//	  llmRef:
//	    name: fast
//	  skills:
//	    - image: registry.ci.openshift.org/ocp/5.0:agentic-skills
//	      paths:
//	        - /skills/cluster-ops
//	  systemPrompt: |
//	    You are an execution agent. Apply the approved remediation plan.
//
// Example — an agent with MCP servers for extended tooling:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Agent
//	metadata:
//	  name: analyzer-with-mcp
//	spec:
//	  llmRef:
//	    name: smart
//	  skills:
//	    - image: registry.ci.openshift.org/ocp/5.0:agentic-skills
//	  mcpServers:
//	    - name: openshift
//	      url: https://mcp.openshift-lightspeed.svc:8443/sse
//	      timeoutSeconds: 10
//	      headers:
//	        - name: Authorization
//	          valueFrom:
//	            type: Kubernetes
//	    - name: pagerduty
//	      url: https://mcp-pagerduty.example.com/sse
//	      headers:
//	        - name: X-API-Key
//	          valueFrom:
//	            type: Secret
//	            secretRef:
//	              name: pagerduty-api-key
//	  systemPrompt: |
//	    You are an SRE analyst with access to MCP tools...
type Agent struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Agent.
	// +required
	Spec AgentSpec `json:"spec,omitzero"`

	// status defines the observed state of Agent.
	// +optional
	Status *AgentStatus `json:"status,omitempty"`
}

const (
	// AgentConditionReady indicates whether all referenced resources
	// (LLMProvider, Secrets, ConfigMaps) exist and are accessible.
	AgentConditionReady string = "Ready"
)

// AgentStatus defines the observed state of Agent. The operator
// validates that all referenced resources exist and reports readiness
// via standard Kubernetes conditions.
type AgentStatus struct {
	// conditions represent the latest available observations of the
	// Agent's state. The Ready condition summarizes whether all
	// referenced resources (LLMProvider, Secrets, ConfigMaps) are present.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// observedGeneration is the most recent generation observed by the
	// operator. It corresponds to the Agent's generation, which is
	// updated on mutation by the API Server.
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true

// AgentList contains a list of Agent.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Agent{}, &AgentList{})
}
