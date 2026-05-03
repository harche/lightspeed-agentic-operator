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
// Each backend has different authentication requirements and API endpoints.
// The type field acts as the discriminator for a union: exactly one of the
// per-provider configuration fields must be set, matching the type value.
//
// Allowed values:
//   - "Anthropic"          — Direct Anthropic API.
//   - "GoogleCloudVertex"  — Google Cloud Vertex AI.
//   - "OpenAI"             — OpenAI-compatible API.
//   - "AzureOpenAI"        — Azure OpenAI Service.
//   - "AWSBedrock"         — AWS Bedrock.
//
// +kubebuilder:validation:Enum=Anthropic;GoogleCloudVertex;OpenAI;AzureOpenAI;AWSBedrock
type LLMProviderType string

const (
	// LLMProviderAnthropic uses the Anthropic API directly.
	LLMProviderAnthropic LLMProviderType = "Anthropic"
	// LLMProviderGoogleCloudVertex uses Google Cloud Vertex AI as the LLM backend.
	LLMProviderGoogleCloudVertex LLMProviderType = "GoogleCloudVertex"
	// LLMProviderOpenAI uses an OpenAI-compatible API endpoint.
	LLMProviderOpenAI LLMProviderType = "OpenAI"
	// LLMProviderAzureOpenAI uses the Azure OpenAI Service.
	LLMProviderAzureOpenAI LLMProviderType = "AzureOpenAI"
	// LLMProviderAWSBedrock uses AWS Bedrock.
	LLMProviderAWSBedrock LLMProviderType = "AWSBedrock"
)

// AnthropicConfig contains configuration for the Anthropic API provider.
type AnthropicConfig struct {
	// credentialsSecret references a Secret containing an ANTHROPIC_API_KEY.
	// Since LLMProvider is cluster-scoped, both name and namespace are required.
	// +required
	CredentialsSecret NamespacedSecretReference `json:"credentialsSecret,omitzero"`
}

// GoogleCloudVertexConfig contains configuration for the Google Cloud Vertex AI provider.
type GoogleCloudVertexConfig struct {
	// credentialsSecret references a Secret containing a service account JSON
	// key (stored under the key GOOGLE_APPLICATION_CREDENTIALS). Since
	// LLMProvider is cluster-scoped, both name and namespace are required.
	// +required
	CredentialsSecret NamespacedSecretReference `json:"credentialsSecret,omitzero"`

	// project is the GCP project ID where Vertex AI is enabled.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Project string `json:"project,omitempty"`

	// region is the GCP region for the Vertex AI endpoint (e.g., "us-central1").
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Region string `json:"region,omitempty"`
}

// OpenAIConfig contains configuration for an OpenAI-compatible API provider.
type OpenAIConfig struct {
	// credentialsSecret references a Secret containing an OPENAI_API_KEY.
	// Since LLMProvider is cluster-scoped, both name and namespace are required.
	// +required
	CredentialsSecret NamespacedSecretReference `json:"credentialsSecret,omitzero"`
}

// AzureOpenAIConfig contains configuration for the Azure OpenAI Service provider.
type AzureOpenAIConfig struct {
	// credentialsSecret references a Secret containing an AZURE_OPENAI_API_KEY.
	// Since LLMProvider is cluster-scoped, both name and namespace are required.
	// +required
	CredentialsSecret NamespacedSecretReference `json:"credentialsSecret,omitzero"`

	// endpoint is the Azure OpenAI resource endpoint
	// (e.g., "https://my-resource.openai.azure.com").
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="endpoint must be a valid HTTP or HTTPS URL"
	Endpoint string `json:"endpoint,omitempty"`

	// apiVersion is the Azure OpenAI API version (e.g., "2024-02-01").
	// When omitted, the SDK default is used.
	// +optional
	// +kubebuilder:validation:MaxLength=32
	APIVersion string `json:"apiVersion,omitempty"`
}

// AWSBedrockConfig contains configuration for the AWS Bedrock provider.
type AWSBedrockConfig struct {
	// credentialsSecret references a Secret containing AWS_ACCESS_KEY_ID
	// and AWS_SECRET_ACCESS_KEY. Since LLMProvider is cluster-scoped,
	// both name and namespace are required.
	// +required
	CredentialsSecret NamespacedSecretReference `json:"credentialsSecret,omitzero"`

	// region is the AWS region for the Bedrock endpoint (e.g., "us-east-1").
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Region string `json:"region,omitempty"`
}

// LLMProviderSpec defines the desired state of LLMProvider.
//
// The type field is the discriminator. Exactly one of the per-provider
// configuration fields (anthropic, googleCloudVertex, openAI, azureOpenAI,
// awsBedrock) must be set, matching the type value.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'Anthropic' ? has(self.anthropic) : !has(self.anthropic)",message="anthropic is required when type is Anthropic, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'GoogleCloudVertex' ? has(self.googleCloudVertex) : !has(self.googleCloudVertex)",message="googleCloudVertex is required when type is GoogleCloudVertex, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'OpenAI' ? has(self.openAI) : !has(self.openAI)",message="openAI is required when type is OpenAI, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'AzureOpenAI' ? has(self.azureOpenAI) : !has(self.azureOpenAI)",message="azureOpenAI is required when type is AzureOpenAI, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'AWSBedrock' ? has(self.awsBedrock) : !has(self.awsBedrock)",message="awsBedrock is required when type is AWSBedrock, and forbidden otherwise"
type LLMProviderSpec struct {
	// type is the LLM provider backend. Determines which per-provider
	// configuration field must be set. Allowed values: "Anthropic",
	// "GoogleCloudVertex", "OpenAI", "AzureOpenAI", "AWSBedrock".
	// +required
	Type LLMProviderType `json:"type,omitempty"`

	// url is an optional override for the provider API endpoint.
	// Most providers have well-known endpoints that the operator resolves
	// automatically, so this is only needed for custom deployments or
	// API proxies. This is not related to the cluster-wide egress proxy
	// (config.openshift.io/v1 Proxy). The operator honors the cluster
	// proxy configuration (HTTP_PROXY, HTTPS_PROXY, NO_PROXY) independently
	// when making requests to the provider endpoint. Must be an HTTP or
	// HTTPS URL, maximum 2048 characters.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="url must be a valid HTTP or HTTPS URL"
	URL string `json:"url,omitempty"`

	// anthropic contains Anthropic-specific configuration.
	// Required when type is "Anthropic".
	// +optional
	Anthropic *AnthropicConfig `json:"anthropic,omitempty"`

	// googleCloudVertex contains Google Cloud Vertex AI-specific configuration.
	// Required when type is "GoogleCloudVertex".
	// +optional
	GoogleCloudVertex *GoogleCloudVertexConfig `json:"googleCloudVertex,omitempty"`

	// openAI contains OpenAI-specific configuration.
	// Required when type is "OpenAI".
	// +optional
	OpenAI *OpenAIConfig `json:"openAI,omitempty"`

	// azureOpenAI contains Azure OpenAI Service-specific configuration.
	// Required when type is "AzureOpenAI".
	// +optional
	AzureOpenAI *AzureOpenAIConfig `json:"azureOpenAI,omitempty"`

	// awsBedrock contains AWS Bedrock-specific configuration.
	// Required when type is "AWSBedrock".
	// +optional
	AWSBedrock *AWSBedrockConfig `json:"awsBedrock,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// LLMProvider defines an LLM provider configuration. It is the first link in
// the CRD chain (LLMProvider -> Agent -> Workflow -> Proposal) and is
// referenced by Agent resources via spec.llmProvider.
//
// LLMProvider is cluster-scoped — the cluster admin manages LLM infrastructure
// centrally. The operator uses the credentials to configure the LLM client
// inside agent sandbox pods. The model is specified on the Agent CR, allowing
// multiple agents to share one LLMProvider with different models.
//
// Typically you create one provider per backend (e.g., one for Vertex AI)
// and then reference it from multiple Agent resources with different models.
//
// Example — a Vertex AI provider (model specified on Agent, not here):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: LLMProvider
//	metadata:
//	  name: vertex-ai
//	spec:
//	  type: GoogleCloudVertex
//	  googleCloudVertex:
//	    credentialsSecret:
//	      name: llm-credentials
//	      namespace: openshift-lightspeed
//	    project: my-gcp-project
//	    region: us-central1
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
