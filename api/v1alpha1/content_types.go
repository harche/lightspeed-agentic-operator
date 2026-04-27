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

// ============================================================================
// Content resource types served by the aggregated content API server.
//
// These resources store large or variable-size data (agent prompts, step
// results) outside the cluster's primary etcd. The aggregated API server
// manages a dedicated storage backend (etcd by default, swappable to S3,
// GCS, PVC, etc.) — API consumers use standard kubectl / client-go and
// never know where data is physically stored.
//
// The CRD types (Proposal, Agent, Workflow) reference content resources
// via ContentReference, keeping the primary CRs small and bounded.
// ============================================================================

// ContentPayload holds the raw content fields shared across all content
// resource specs. Either content (text) or data (binary) should be set.
type ContentPayload struct {
	// mediaType identifies the content format (e.g., "text/plain",
	// "image/png", "application/pdf"). Defaults to "text/plain" when
	// omitted. Must be 1-256 characters.
	// +optional
	// +kubebuilder:validation:MaxLength=256
	MediaType string `json:"mediaType,omitempty"`

	// content is the text payload.
	// +optional
	Content string `json:"content,omitempty"`

	// data is the binary payload (base64-encoded in JSON) for non-text
	// content such as images, PDFs, or other files.
	// +optional
	Data []byte `json:"data,omitempty"`
}

// +kubebuilder:object:root=true

// RequestContent stores the request payload for a proposal. Referenced by
// Proposal.spec.request via ContentReference. The adapter or user creates
// this resource with the alert description, incident details, or user
// request, then references it from the Proposal.
//
// Supports both text and binary content (images, PDFs, etc.) via the
// mediaType field. The agent receives the content in the format
// indicated by mediaType.
//
// Example — text request:
//
//	apiVersion: content.agentic.openshift.io/v1alpha1
//	kind: RequestContent
//	metadata:
//	  name: fix-crashloop-request
//	spec:
//	  mediaType: text/plain
//	  content: |
//	    Pod web-frontend-5d4b8c6f-x9k2m in namespace production is in
//	    CrashLoopBackOff. Last restart reason: OOMKilled.
//
// Example — image attachment:
//
//	apiVersion: content.agentic.openshift.io/v1alpha1
//	kind: RequestContent
//	metadata:
//	  name: dashboard-screenshot
//	spec:
//	  mediaType: image/png
//	  data: <base64-encoded PNG>
type RequestContent struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the request content.
	// +required
	Spec RequestContentSpec `json:"spec,omitzero"`
}

// RequestContentSpec holds the request payload.
type RequestContentSpec struct {
	ContentPayload `json:",inline"`
}

// +kubebuilder:object:root=true

// RequestContentList contains a list of RequestContent.
type RequestContentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RequestContent `json:"items"`
}

// +kubebuilder:object:root=true

// AnalysisResult stores the full output of an analysis step. Referenced
// by AnalysisStepStatus.result via ContentReference.
//
// The operator creates this resource after a successful analysis step.
// The console UI fetches it to display remediation options to the user.
type AnalysisResult struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec contains the analysis agent's output.
	// +required
	Spec AnalysisResultSpec `json:"spec,omitzero"`
}

// AnalysisResultSpec holds the full analysis output.
type AnalysisResultSpec struct {
	ContentPayload `json:",inline"`

	// options contains one or more remediation options returned by the
	// analysis agent. Each option has its own diagnosis, plan,
	// verification strategy, and RBAC requirements.
	// +required
	// +listType=atomic
	Options []RemediationOption `json:"options,omitempty"`

	// components contains optional adapter-specific UI components that
	// apply to the analysis step as a whole (not to a specific option).
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	Components []apiextensionsv1.JSON `json:"components,omitempty"`
}

// +kubebuilder:object:root=true

// AnalysisResultList contains a list of AnalysisResult.
type AnalysisResultList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AnalysisResult `json:"items"`
}

// +kubebuilder:object:root=true

// ExecutionResult stores the full output of an execution step. Referenced
// by ExecutionStepStatus.result via ContentReference.
type ExecutionResult struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec contains the execution agent's output.
	// +required
	Spec ExecutionResultSpec `json:"spec,omitzero"`
}

// ExecutionResultSpec holds the full execution output.
type ExecutionResultSpec struct {
	ContentPayload `json:",inline"`

	// actionsTaken lists what the agent did.
	// +optional
	// +listType=atomic
	ActionsTaken []ExecutionAction `json:"actionsTaken,omitempty"`

	// verification is the lightweight inline verification the execution
	// agent performs immediately after completing its actions.
	// +optional
	Verification *ExecutionVerification `json:"verification,omitempty"`

	// components contains optional adapter-defined structured data.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	Components []apiextensionsv1.JSON `json:"components,omitempty"`
}

// +kubebuilder:object:root=true

// ExecutionResultList contains a list of ExecutionResult.
type ExecutionResultList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExecutionResult `json:"items"`
}

// +kubebuilder:object:root=true

// VerificationResult stores the full output of a verification step.
// Referenced by VerificationStepStatus.result via ContentReference.
type VerificationResult struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec contains the verification agent's output.
	// +required
	Spec VerificationResultSpec `json:"spec,omitzero"`
}

// VerificationResultSpec holds the full verification output.
type VerificationResultSpec struct {
	ContentPayload `json:",inline"`

	// checks contains individual verification check results.
	// +optional
	// +listType=atomic
	Checks []VerifyCheck `json:"checks,omitempty"`

	// summary is a Markdown-formatted verification summary.
	// +optional
	Summary string `json:"summary,omitempty"`

	// components contains optional adapter-defined structured data.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	Components []apiextensionsv1.JSON `json:"components,omitempty"`
}

// +kubebuilder:object:root=true

// VerificationResultList contains a list of VerificationResult.
type VerificationResultList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerificationResult `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&RequestContent{}, &RequestContentList{},
		&AnalysisResult{}, &AnalysisResultList{},
		&ExecutionResult{}, &ExecutionResultList{},
		&VerificationResult{}, &VerificationResultList{},
	)
}
