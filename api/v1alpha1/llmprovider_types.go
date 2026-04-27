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

// LLMProviderType identifies the hosting backend for an LLM provider.
//
// Each backend has different authentication requirements and API endpoints:
//   - "Anthropic"    — Direct Anthropic API. Secret needs ANTHROPIC_API_KEY.
//   - "Vertex"       — Google Cloud Vertex AI. Secret needs service account JSON
//     (GOOGLE_APPLICATION_CREDENTIALS) plus GCP_PROJECT and GCP_REGION.
//   - "OpenAI"       — OpenAI-compatible API. Secret needs OPENAI_API_KEY.
//   - "AzureOpenAI"  — Azure OpenAI Service. Secret needs AZURE_OPENAI_API_KEY,
//     AZURE_OPENAI_ENDPOINT, and optionally AZURE_OPENAI_API_VERSION.
//   - "Bedrock"      — AWS Bedrock. Secret needs AWS_ACCESS_KEY_ID,
//     AWS_SECRET_ACCESS_KEY, and AWS_REGION.
//
// +kubebuilder:validation:Enum=Anthropic;Vertex;OpenAI;AzureOpenAI;Bedrock
type LLMProviderType string

const (
	// LLMProviderAnthropic uses the Anthropic API directly.
	LLMProviderAnthropic LLMProviderType = "Anthropic"
	// LLMProviderVertex uses Google Cloud Vertex AI as the LLM backend.
	LLMProviderVertex LLMProviderType = "Vertex"
	// LLMProviderOpenAI uses an OpenAI-compatible API endpoint.
	LLMProviderOpenAI LLMProviderType = "OpenAI"
	// LLMProviderAzureOpenAI uses the Azure OpenAI Service.
	LLMProviderAzureOpenAI LLMProviderType = "AzureOpenAI"
	// LLMProviderBedrock uses AWS Bedrock.
	LLMProviderBedrock LLMProviderType = "Bedrock"
)

// LLMProviderSpec defines the desired state of LLMProvider.
type LLMProviderSpec struct {
	// type is the LLM provider backend (e.g., "vertex", "anthropic", "bedrock").
	// See LLMProviderType for the authentication requirements of each backend.
	// +required
	Type LLMProviderType `json:"type,omitempty"`

	// credentialsSecret references a Secret in the operator's namespace
	// containing the provider credentials. The required keys depend on the
	// provider type (see LLMProviderType for details). The operator reads this
	// secret and injects the credentials into agent sandbox pods at runtime.
	// +required
	CredentialsSecret SecretReference `json:"credentialsSecret,omitempty"`

	// model is the LLM model identifier as recognized by the provider
	// (e.g., "claude-opus-4-6", "claude-haiku-4-5", "gpt-4o").
	// Different agents can reference different LLMProviders to use different
	// models for different tasks (e.g., a capable model for analysis,
	// a fast model for execution). Must be 1-256 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Model string `json:"model,omitempty"`

	// url is an optional override for the provider API endpoint.
	// Most providers have well-known endpoints that the operator resolves
	// automatically, so this is only needed for custom deployments or
	// API proxies. This is not related to the cluster-wide egress proxy
	// (config.openshift.io/v1 Proxy). The operator honors the cluster
	// proxy configuration (HTTP_PROXY, HTTPS_PROXY, NO_PROXY) independently
	// when making requests to the provider endpoint. Must be an HTTP or
	// HTTPS URL, maximum 2048 characters.
	// +optional
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Pattern=`^https?://.*$`
	URL *string `json:"url,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.model`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// LLMProvider defines an LLM provider configuration. It is the first link in
// the CRD chain (LLMProvider -> Agent -> Workflow -> Proposal) and is
// referenced by Agent resources via spec.llmProvider.
//
// LLMProvider is namespace-scoped for multi-tenancy. The operator uses the
// credentials and model to configure the LLM client inside agent sandbox
// pods. All resources in the CRD chain must be in the same namespace.
//
// Typically you create a small number of providers representing different
// capability/cost tiers (e.g., "smart" for complex analysis, "fast" for
// routine execution) and then reference them from multiple Agent resources.
//
// Example — a high-capability provider for analysis tasks:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: LLMProvider
//	metadata:
//	  name: smart
//	spec:
//	  type: vertex
//	  model: claude-opus-4-6
//	  credentialsSecret:
//	    name: llm-credentials
//
// Example — a fast, cost-efficient provider for execution tasks:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: LLMProvider
//	metadata:
//	  name: fast
//	spec:
//	  type: vertex
//	  model: claude-haiku-4-5
//	  credentialsSecret:
//	    name: llm-credentials
type LLMProvider struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of LLMProvider.
	// +required
	Spec LLMProviderSpec `json:"spec,omitzero"`
}

// +kubebuilder:object:root=true

// LLMProviderList contains a list of LLMProvider.
type LLMProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LLMProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LLMProvider{}, &LLMProviderList{})
}
