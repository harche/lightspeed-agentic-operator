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

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Proposal",type=string,JSONPath=`.proposalName`
// +kubebuilder:printcolumn:name="Attempt",type=integer,JSONPath=`.attempt`
// +kubebuilder:printcolumn:name="Retry",type=integer,JSONPath=`.retryIndex`
// +kubebuilder:printcolumn:name="Outcome",type=string,JSONPath=`.status.conditions[?(@.type=="Completed")].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VerificationResult records the output of a single verification step
// execution. Created by the operator after the verification agent
// completes. Owned by the parent Proposal for garbage collection.
type VerificationResult struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// proposalName is the name of the parent Proposal in the same namespace.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	ProposalName string `json:"proposalName"`

	// attempt is the 1-based overall attempt number.
	// +required
	// +kubebuilder:validation:Minimum=1
	Attempt int32 `json:"attempt"`

	// retryIndex is the 0-based retry index within the current analysis.
	// +required
	// +kubebuilder:validation:Minimum=0
	RetryIndex int32 `json:"retryIndex"`

	// checks contains individual verification check results.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=50
	Checks []VerifyCheck `json:"checks,omitempty"`

	// summary is a Markdown-formatted verification summary.
	// +optional
	// +kubebuilder:validation:MaxLength=32768
	Summary string `json:"summary,omitempty"`

	// components contains optional adapter-defined structured data.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	Components []apiextensionsv1.JSON `json:"components,omitempty"`

	// sandbox tracks the sandbox pod used for this verification.
	// +optional
	Sandbox SandboxInfo `json:"sandbox,omitzero"`

	// failureReason is populated when the step failed due to a system error.
	// +optional
	// +kubebuilder:validation:MaxLength=8192
	FailureReason string `json:"failureReason,omitempty"`

	// status contains conditions tracking the result lifecycle.
	// +optional
	Status ResultStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VerificationResultList contains a list of VerificationResult.
type VerificationResultList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerificationResult `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerificationResult{}, &VerificationResultList{})
}
