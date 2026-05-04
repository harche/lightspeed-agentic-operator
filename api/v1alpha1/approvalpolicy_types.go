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

// ApprovalMode controls whether a step requires explicit user approval.
// +kubebuilder:validation:Enum=Automatic;Manual
type ApprovalMode string

const (
	ApprovalModeAutomatic ApprovalMode = "Automatic"
	ApprovalModeManual    ApprovalMode = "Manual"
)

// ApprovalPolicyStage configures the approval mode for a single workflow step.
type ApprovalPolicyStage struct {
	// name is the workflow step this policy applies to.
	// +required
	Name SandboxStep `json:"name"`

	// approval controls whether this step auto-approves or requires
	// explicit user approval on the ProposalApproval resource.
	// +required
	Approval ApprovalMode `json:"approval"`
}

// ApprovalPolicySpec defines the desired state of ApprovalPolicy.
//
// +kubebuilder:validation:XValidation:rule="self.stages.all(s1, self.stages.all(s2, s1 == s2 || s1.name != s2.name))",message="stage names must be unique"
type ApprovalPolicySpec struct {
	// stages configures the approval mode for each workflow step.
	// Omitted steps default to Manual.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=4
	Stages []ApprovalPolicyStage `json:"stages,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ApprovalPolicy is a cluster-scoped singleton that configures default
// approval behavior for proposal workflow steps. The cluster admin creates
// a single ApprovalPolicy named "cluster" to control which steps auto-approve.
//
// Steps not listed in the policy default to Manual (require explicit
// user approval on the ProposalApproval resource).
//
// Example:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: ApprovalPolicy
//	metadata:
//	  name: cluster
//	spec:
//	  stages:
//	    - name: Analysis
//	      approval: Automatic
//	    - name: Execution
//	      approval: Manual
//	    - name: Verification
//	      approval: Automatic
type ApprovalPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec ApprovalPolicySpec `json:"spec,omitzero"`
}

// +kubebuilder:object:root=true

// ApprovalPolicyList contains a list of ApprovalPolicy.
type ApprovalPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApprovalPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ApprovalPolicy{}, &ApprovalPolicyList{})
}
