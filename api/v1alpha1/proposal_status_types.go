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

// ActionOutcome indicates whether an individual execution action succeeded.
// +kubebuilder:validation:Enum=Succeeded;Failed
type ActionOutcome string

const (
	ActionOutcomeSucceeded ActionOutcome = "Succeeded"
	ActionOutcomeFailed    ActionOutcome = "Failed"
)

// ConditionOutcome indicates whether the target condition improved after remediation.
// +kubebuilder:validation:Enum=Improved;Unchanged;Degraded
type ConditionOutcome string

const (
	ConditionOutcomeImproved  ConditionOutcome = "Improved"
	ConditionOutcomeUnchanged ConditionOutcome = "Unchanged"
	ConditionOutcomeDegraded  ConditionOutcome = "Degraded"
)

// CheckResult indicates whether a verification check passed.
// +kubebuilder:validation:Enum=Passed;Failed
type CheckResult string

const (
	CheckResultPassed CheckResult = "Passed"
	CheckResultFailed CheckResult = "Failed"
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
	// outcome indicates whether this individual action succeeded.
	// Must be one of: Succeeded, Failed.
	// +optional
	Outcome ActionOutcome `json:"outcome,omitempty"`
	// output is the command output or API response from the action.
	// Maximum 32768 characters.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32768
	Output string `json:"output,omitempty"`
	// error is the error message if the action failed.
	// Maximum 8192 characters.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=8192
	Error string `json:"error,omitempty"`
}

// ExecutionVerification is a lightweight inline verification that the
// execution agent performs immediately after completing its actions,
// before the formal verification step. This gives early signal on whether
// the remediation worked. In trust-mode workflows (verification skipped),
// this is the only verification that occurs.
type ExecutionVerification struct {
	// conditionOutcome indicates whether the target condition improved
	// after the remediation (e.g., pod is no longer CrashLoopBackOff).
	// Must be one of: Improved, Unchanged, Degraded.
	// +optional
	ConditionOutcome ConditionOutcome `json:"conditionOutcome,omitempty"`
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
	// result indicates whether the check's observed value matches
	// the expected value. Must be one of: Passed, Failed.
	// +optional
	Result CheckResult `json:"result,omitempty"`
}

// SandboxInfo tracks the sandbox pod used for a workflow step. The operator
// creates a sandbox pod for each active step (analysis, execution,
// verification) and records the claim details here. This enables the
// console UI to stream sandbox pod logs in real time.
type SandboxInfo struct {
	// claimName is the name of the SandboxClaim resource that owns the
	// sandbox pod. Omit when no sandbox has been claimed; an empty string
	// is treated the same as omitted. Maximum 253 characters.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	ClaimName string `json:"claimName,omitempty"`
	// namespace is the namespace where the SandboxClaim and its pod live.
	// Must be a valid RFC 1123 DNS label.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="!format.dns1123Label().validate(self).hasValue()",message="must be a valid DNS label: lowercase alphanumeric characters and hyphens, starting with an alphabetic character and ending with an alphanumeric character"
	Namespace string `json:"namespace,omitempty"`
	// startTime is when the sandbox pod was created.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// completionTime is when the sandbox pod finished (success or failure).
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// StepResultRef is a lightweight reference to a result CR with an inline
// success field for quick scanning without fetching the CR.
type StepResultRef struct {
	// name is the name of the result CR.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// success indicates the outcome of this step attempt.
	// +required
	Success bool `json:"success"`
}

// AnalysisStepStatus is the observed state of the analysis step.
type AnalysisStepStatus struct {
	// conditions for this step.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	// startTime is when the step started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// completionTime is when the step completed.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// sandbox tracks the sandbox used.
	// +optional
	Sandbox SandboxInfo `json:"sandbox,omitzero"`
	// selectedOption is the 0-based index into the options array
	// of the latest AnalysisResult CR that the user approved.
	// +optional
	// +kubebuilder:validation:Minimum=0
	SelectedOption *int32 `json:"selectedOption,omitempty"`
	// observedRevision is the revision number from spec.revision that this
	// analysis was produced for. When spec.revision > observedRevision,
	// the operator re-runs analysis with revision context.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedRevision *int32 `json:"observedRevision,omitempty"`
	// results references AnalysisResult CRs, newest last.
	// Each entry corresponds to one analysis attempt.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	Results []StepResultRef `json:"results,omitempty"`
}

// ExecutionStepStatus is the observed state of the execution step.
type ExecutionStepStatus struct {
	// conditions for this step.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	// startTime is when the step started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// completionTime is when the step completed.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// sandbox tracks the sandbox used.
	// +optional
	Sandbox SandboxInfo `json:"sandbox,omitzero"`
	// retryCount tracks how many times execution+verification has been
	// retried for the current analysis option. Reset when a new analysis
	// is run (initial or revision). The operator increments this on each
	// objective verification failure before retrying execution.
	// +optional
	// +kubebuilder:validation:Minimum=0
	RetryCount *int32 `json:"retryCount,omitempty"`
	// results references ExecutionResult CRs, newest last.
	// Each entry corresponds to one execution attempt (including retries).
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	Results []StepResultRef `json:"results,omitempty"`
}

// VerificationStepStatus is the observed state of the verification step.
type VerificationStepStatus struct {
	// conditions for this step.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	// startTime is when the step started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// completionTime is when the step completed.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// sandbox tracks the sandbox used.
	// +optional
	Sandbox SandboxInfo `json:"sandbox,omitzero"`
	// results references VerificationResult CRs, newest last.
	// Each entry corresponds to one verification attempt (including retries).
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	Results []StepResultRef `json:"results,omitempty"`
}

// StepsStatus contains the per-step observed state for all three workflow
// steps. Each step status is populated independently as the proposal
// progresses through its lifecycle. All fields are set by the operator.
type StepsStatus struct {
	// analysis is the observed state of the analysis step.
	// +optional
	Analysis AnalysisStepStatus `json:"analysis,omitzero"`
	// execution is the observed state of the execution step.
	// +optional
	Execution ExecutionStepStatus `json:"execution,omitzero"`
	// verification is the observed state of the verification step.
	// +optional
	Verification VerificationStepStatus `json:"verification,omitzero"`
}
