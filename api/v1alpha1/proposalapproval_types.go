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

// ApprovalStageType identifies which workflow step an approval entry applies to.
// +kubebuilder:validation:Enum=Analysis;Execution;Verification
type ApprovalStageType string

const (
	ApprovalStageAnalysis     ApprovalStageType = "Analysis"
	ApprovalStageExecution    ApprovalStageType = "Execution"
	ApprovalStageVerification ApprovalStageType = "Verification"
)

// AnalysisApproval contains approval parameters for the analysis step.
type AnalysisApproval struct {
	// agent overrides the Agent CR for this step, enabling cost control.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Agent string `json:"agent,omitempty"`
}

// ExecutionApproval contains approval parameters for the execution step.
type ExecutionApproval struct {
	// agent overrides the Agent CR for this step, enabling cost control.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Agent string `json:"agent,omitempty"`

	// option is the 0-based index into the analysis options array
	// selecting which remediation approach to execute.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Option *int32 `json:"option,omitempty"`
}

// VerificationApproval contains approval parameters for the verification step.
type VerificationApproval struct {
	// agent overrides the Agent CR for this step, enabling cost control.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Agent string `json:"agent,omitempty"`
}

// ApprovalStage is a discriminated union representing approval for one
// workflow step. Presence in spec.stages indicates approval; absence means
// not yet approved (controller checks ApprovalPolicy for auto-approve).
//
// +kubebuilder:validation:XValidation:rule="self.type == 'Analysis' ? has(self.analysis) : !has(self.analysis)",message="analysis is required when type is Analysis, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'Execution' ? has(self.execution) : !has(self.execution)",message="execution is required when type is Execution, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'Verification' ? has(self.verification) : !has(self.verification)",message="verification is required when type is Verification, and forbidden otherwise"
type ApprovalStage struct {
	// type identifies which workflow step this approval is for.
	// +required
	Type ApprovalStageType `json:"type"`

	// denied indicates the user has denied this step, making the entire
	// proposal terminal. Once set to true, it cannot be unset.
	// +optional
	Denied bool `json:"denied,omitempty"`

	// analysis contains approval parameters for the analysis step.
	// Required when type is Analysis.
	// +optional
	Analysis *AnalysisApproval `json:"analysis,omitempty"`

	// execution contains approval parameters for the execution step.
	// Required when type is Execution.
	// +optional
	Execution *ExecutionApproval `json:"execution,omitempty"`

	// verification contains approval parameters for the verification step.
	// Required when type is Verification.
	// +optional
	Verification *VerificationApproval `json:"verification,omitempty"`
}

// ProposalApprovalSpec defines the desired state of ProposalApproval.
//
// spec.stages is append-only: once a stage is added, it cannot be removed.
// Once denied is set to true on a stage, it cannot be unset.
//
// +kubebuilder:validation:XValidation:rule="oldSelf.stages.all(old, self.stages.exists(s, s.type == old.type))",message="stages are append-only: existing stages cannot be removed"
// +kubebuilder:validation:XValidation:rule="oldSelf.stages.all(old, !(has(old.denied) && old.denied) || self.stages.exists(s, s.type == old.type && has(s.denied) && s.denied))",message="denied cannot be unset once set to true"
// +kubebuilder:validation:XValidation:rule="self.stages.all(s1, self.stages.all(s2, s1 == s2 || s1.type != s2.type))",message="stage types must be unique"
type ProposalApprovalSpec struct {
	// stages lists the approved (or denied) workflow steps. Each entry is
	// a discriminated union keyed by type.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=3
	Stages []ApprovalStage `json:"stages,omitempty"`
}

// ApprovalStageStatus is the observed state of a single approval stage.
type ApprovalStageStatus struct {
	// name identifies the workflow step.
	// +required
	Name string `json:"name"`

	// conditions for this approval stage.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// ProposalApprovalStatus defines the observed state of ProposalApproval.
type ProposalApprovalStatus struct {
	// stages contains the per-stage approval status set by the controller.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=3
	Stages []ApprovalStageStatus `json:"stages,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProposalApproval tracks per-step approval state for a Proposal. The
// operator creates it when a Proposal is created. Users update it to
// approve or deny individual workflow steps.
//
// ProposalApproval has a 1:1 relationship with its Proposal (same name,
// same namespace) and is owned by the Proposal via an owner reference
// for garbage collection.
//
// Example:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: ProposalApproval
//	metadata:
//	  name: fix-crash
//	  namespace: my-namespace
//	  ownerReferences:
//	    - apiVersion: agentic.openshift.io/v1alpha1
//	      kind: Proposal
//	      name: fix-crash
//	spec:
//	  stages:
//	    - type: Analysis
//	      analysis: {}
//	    - type: Execution
//	      execution:
//	        option: 0
//	        agent: fast
type ProposalApproval struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ProposalApprovalSpec `json:"spec,omitzero"`

	// +optional
	Status ProposalApprovalStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ProposalApprovalList contains a list of ProposalApproval.
type ProposalApprovalList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProposalApproval `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProposalApproval{}, &ProposalApprovalList{})
}
