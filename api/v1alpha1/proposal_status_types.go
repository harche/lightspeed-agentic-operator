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

// ExecutionAction describes a single action taken by the execution agent
// during the execution step. These are recorded in ExecutionStepStatus
// to provide an audit trail of what the agent actually did.
type ExecutionAction struct {
	// type is the action category (e.g., "patch", "scale", "restart").
	// Maximum 256 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Type string `json:"type,omitempty"`
	// description is a Markdown-formatted explanation of what the agent did
	// (e.g., "Patched deployment/web to set memory limit to 512Mi").
	// Maximum 4096 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Description string `json:"description,omitempty"`
	// success indicates whether this individual action succeeded.
	// +optional
	Success *bool `json:"success,omitempty"`
	// output is the command output or API response from the action.
	// Maximum 32768 characters.
	// +optional
	// +kubebuilder:validation:MaxLength=32768
	Output *string `json:"output,omitempty"`
	// error is the error message if the action failed.
	// Maximum 8192 characters.
	// +optional
	// +kubebuilder:validation:MaxLength=8192
	Error *string `json:"error,omitempty"`
}

// ExecutionVerification is a lightweight inline verification that the
// execution agent performs immediately after completing its actions,
// before the formal verification step. This gives early signal on whether
// the remediation worked. In trust-mode workflows (verification skipped),
// this is the only verification that occurs.
type ExecutionVerification struct {
	// conditionImproved indicates whether the target condition improved
	// after the remediation (e.g., pod is no longer CrashLoopBackOff).
	// +optional
	ConditionImproved *bool `json:"conditionImproved,omitempty"`
	// summary is a Markdown-formatted summary of the inline verification.
	// Maximum 4096 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Summary string `json:"summary,omitempty"`
}

// VerifyCheck is a single verification check result from the verification
// agent. Each check corresponds to a VerificationStep from the analysis
// agent's verification plan.
type VerifyCheck struct {
	// name is the check identifier, matching the VerificationStep name.
	// Maximum 253 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
	// source is what performed the check (e.g., "oc", "promql", "curl").
	// Maximum 256 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Source string `json:"source,omitempty"`
	// value is the actual observed value (e.g., "Running", "3 replicas").
	// Maximum 4096 characters.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Value string `json:"value,omitempty"`
	// passed indicates whether the check's observed value matches
	// the expected value.
	// +optional
	Passed *bool `json:"passed,omitempty"`
}

// SandboxInfo tracks the sandbox pod used for a workflow step. The operator
// creates a sandbox pod for each active step (analysis, execution,
// verification) and records the claim details here. This enables the
// console UI to stream sandbox pod logs in real time.
type SandboxInfo struct {
	// claimName is the name of the SandboxClaim resource that owns the
	// sandbox pod. Maximum 253 characters.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	ClaimName *string `json:"claimName,omitempty"`
	// namespace is the namespace where the SandboxClaim and its pod live.
	// Maximum 253 characters.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Namespace *string `json:"namespace,omitempty"`
	// startTime is when the sandbox pod was created.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// completionTime is when the sandbox pod finished (success or failure).
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// AnalysisStepStatus is the observed state of the analysis step.
// All fields are populated by the operator based on the analysis agent's
// output. The options field is the most important -- it contains the
// remediation options the user chooses from after analysis.
type AnalysisStepStatus struct {
	// conditions for this step.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	// startTime is when the step started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// completionTime is when the step completed.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// sandbox tracks the sandbox used.
	// +optional
	Sandbox *SandboxInfo `json:"sandbox,omitempty"`
	// options contains one or more remediation options returned by the
	// analysis agent. Each option has its own diagnosis, plan, verification
	// strategy, and RBAC requirements. The user reviews these in the
	// analysis and selects one to approve. Maximum 10 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=10
	Options []RemediationOption `json:"options,omitempty"`
	// selectedOption is the 0-based index into the options array that the
	// user approved. Set when the user approves the proposal. The operator
	// uses this to determine which option's RBAC and plan to use for
	// execution. Minimum value: 0.
	// +optional
	// +kubebuilder:validation:Minimum=0
	SelectedOption *int32 `json:"selectedOption,omitempty"`
	// components contains optional adapter-specific UI components that
	// apply to the analysis step as a whole (not to a specific option).
	// Maximum 20 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	Components []apiextensionsv1.JSON `json:"components,omitempty"`
}

// ExecutionStepStatus is the observed state of the execution step.
// Populated by the operator from the execution agent's output. Contains
// an audit trail of every action the agent took and whether each succeeded.
type ExecutionStepStatus struct {
	// conditions for this step.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	// startTime is when the step started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// completionTime is when the step completed.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// sandbox tracks the sandbox used.
	// +optional
	Sandbox *SandboxInfo `json:"sandbox,omitempty"`
	// actionsTaken lists what the agent did. Maximum 100 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=100
	ActionsTaken []ExecutionAction `json:"actionsTaken,omitempty"`
	// verification is the inline verification from the execution agent.
	// +optional
	Verification *ExecutionVerification `json:"verification,omitempty"`
	// components contains optional adapter-defined structured data.
	// Maximum 20 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	Components []apiextensionsv1.JSON `json:"components,omitempty"`
}

// VerificationStepStatus is the observed state of the verification step.
// Populated by the operator from the verification agent's output. Contains
// individual check results and an overall assessment via conditions.
type VerificationStepStatus struct {
	// conditions for this step.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	// startTime is when the step started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// completionTime is when the step completed.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// sandbox tracks the sandbox used.
	// +optional
	Sandbox *SandboxInfo `json:"sandbox,omitempty"`
	// checks contains individual verification check results.
	// Maximum 20 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	Checks []VerifyCheck `json:"checks,omitempty"`
	// summary is a Markdown-formatted verification summary.
	// Maximum 8192 characters.
	// +optional
	// +kubebuilder:validation:MaxLength=8192
	Summary *string `json:"summary,omitempty"`
	// components contains optional adapter-defined structured data.
	// Maximum 20 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	Components []apiextensionsv1.JSON `json:"components,omitempty"`
}

// StepsStatus contains the per-step observed state for all three workflow
// steps. Each step status is populated independently as the proposal
// progresses through its lifecycle. All fields are set by the operator.
type StepsStatus struct {
	// analysis is the observed state of the analysis step.
	// +optional
	Analysis *AnalysisStepStatus `json:"analysis,omitempty"`
	// execution is the observed state of the execution step.
	// +optional
	Execution *ExecutionStepStatus `json:"execution,omitempty"`
	// verification is the observed state of the verification step.
	// +optional
	Verification *VerificationStepStatus `json:"verification,omitempty"`
}
