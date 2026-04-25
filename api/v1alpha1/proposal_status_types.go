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
// The full analysis output (remediation options, components) is stored
// via the aggregated content API and referenced by result. The Proposal
// status holds only conditions, timing, and the user's selection.
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
	// result references the full analysis output (remediation options,
	// components) stored via the aggregated content API. The operator
	// populates this after a successful analysis step.
	// +optional
	Result *ContentReference `json:"result,omitempty"`
	// selectedOption is the 0-based index into the result's options array
	// that the user approved. Set when the user approves the proposal.
	// The operator uses this to determine which option's RBAC and plan
	// to use for execution. Minimum value: 0.
	// +optional
	// +kubebuilder:validation:Minimum=0
	SelectedOption *int32 `json:"selectedOption,omitempty"`
	// observedRevision is the revision number from spec.revision that this
	// analysis was produced for. When spec.revision > observedRevision,
	// the operator re-runs analysis with revision context.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedRevision *int32 `json:"observedRevision,omitempty"`
}

// ExecutionStepStatus is the observed state of the execution step.
// The full execution output (actions taken, inline verification) is
// stored via the aggregated content API and referenced by result.
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
	// result references the full execution output (actions taken,
	// inline verification, components) stored via the aggregated
	// content API. The operator populates this after execution completes.
	// +optional
	Result *ContentReference `json:"result,omitempty"`
	// retryCount tracks how many times execution+verification has been
	// retried for the current analysis option. Reset when a new analysis
	// is run (initial or revision). The operator increments this on each
	// objective verification failure before retrying execution.
	// +optional
	// +kubebuilder:validation:Minimum=0
	RetryCount *int32 `json:"retryCount,omitempty"`
}

// VerificationStepStatus is the observed state of the verification step.
// The full verification output (checks, summary) is stored via the
// aggregated content API and referenced by result.
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
	// result references the full verification output (checks, summary,
	// components) stored via the aggregated content API. The operator
	// populates this after verification completes.
	// +optional
	Result *ContentReference `json:"result,omitempty"`
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
