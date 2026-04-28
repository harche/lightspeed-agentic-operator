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

// ProposalPhase summarizes the proposal's lifecycle state for display.
// The operator derives this from conditions and sets it on every reconcile.
// Conditions remain the source of truth; this field is for human consumption
// (e.g., oc get proposals, console UI list views).
// +kubebuilder:validation:Enum=Pending;Analyzing;Proposed;Approved;Executing;AwaitingSync;Verifying;Completed;Failed;Denied;Escalated
type ProposalPhase string

const (
	ProposalPhasePending      ProposalPhase = "Pending"
	ProposalPhaseAnalyzing    ProposalPhase = "Analyzing"
	ProposalPhaseProposed     ProposalPhase = "Proposed"
	ProposalPhaseApproved     ProposalPhase = "Approved"
	ProposalPhaseExecuting    ProposalPhase = "Executing"
	ProposalPhaseAwaitingSync ProposalPhase = "AwaitingSync"
	ProposalPhaseVerifying    ProposalPhase = "Verifying"
	ProposalPhaseCompleted    ProposalPhase = "Completed"
	ProposalPhaseFailed       ProposalPhase = "Failed"
	ProposalPhaseDenied       ProposalPhase = "Denied"
	ProposalPhaseEscalated    ProposalPhase = "Escalated"
)

// SandboxStep identifies which workflow step a sandbox pod is running for.
// Used in PreviousAttempt to record which step failed, and internally by the
// operator for sandbox lifecycle management.
// +kubebuilder:validation:Enum=Analysis;Execution;Verification
type SandboxStep string

const (
	// SandboxStepAnalysis is the analysis step sandbox.
	SandboxStepAnalysis SandboxStep = "Analysis"
	// SandboxStepExecution is the execution step sandbox.
	SandboxStepExecution SandboxStep = "Execution"
	// SandboxStepVerification is the verification step sandbox.
	SandboxStepVerification SandboxStep = "Verification"
)

// Condition types for Proposal. Conditions are the primary mechanism for
// observing proposal state. The operator sets these as the proposal
// progresses through its lifecycle. Each condition has a type, status
// (True/False/Unknown), reason (CamelCase token), and message.
//
// The lifecycle is derived from the combination of conditions:
//
//	No conditions          -> just created, pending
//	Analyzed=Unknown       -> analysis in progress
//	Analyzed=True          -> analysis complete, awaiting approval
//	Approved=True          -> user approved; execution pending or in progress
//	Approved=False         -> user denied (terminal)
//	Executed=True          -> execution complete
//	AwaitingSync=True      -> execution skipped, manual sync needed
//	Verified=True          -> verification passed (terminal: success)
//	Escalated=True         -> max retries exhausted (terminal)
//	Any condition=False    -> step failed; check reason and message
const (
	// ProposalConditionAnalyzed indicates whether analysis has completed.
	// Status=True when analysis succeeds, Status=False on failure,
	// Status=Unknown while analysis is in progress.
	ProposalConditionAnalyzed string = "Analyzed"
	// ProposalConditionApproved indicates the user's approval decision.
	// Status=True when approved, Status=False when denied.
	ProposalConditionApproved string = "Approved"
	// ProposalConditionExecuted indicates whether execution has completed.
	// Status=True when execution succeeds, Status=False on failure,
	// Status=Unknown while execution is in progress.
	ProposalConditionExecuted string = "Executed"
	// ProposalConditionAwaitingSync indicates that execution was skipped
	// and the user is expected to apply changes manually or via GitOps.
	// Status=True when awaiting sync, removed when synced.
	ProposalConditionAwaitingSync string = "AwaitingSync"
	// ProposalConditionVerified indicates whether verification has passed.
	// Status=True when verification succeeds, Status=False on failure,
	// Status=Unknown while verification is in progress.
	ProposalConditionVerified string = "Verified"
	// ProposalConditionEscalated indicates the proposal exhausted all retry
	// attempts and a child proposal was created.
	ProposalConditionEscalated string = "Escalated"
)

// ProposalStep defines per-step configuration on a Proposal. When used
// inline (no templateRef), the agent field selects the agent tier. The
// tools field provides per-step tools that replace the shared spec.tools.
type ProposalStep struct {
	// agent is the name of the cluster-scoped Agent to use for this step.
	// Only meaningful when the Proposal defines steps inline (no templateRef).
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Agent string `json:"agent,omitempty"`

	// tools provides per-step tools that replace the shared spec.tools
	// for this step. Use this when different steps need different skills.
	// +optional
	Tools *ToolsSpec `json:"tools,omitempty"`
}

// PreviousAttempt captures the state of a failed attempt. When a proposal
// fails and retries, the operator records the failure context here so that
// the analysis agent on the next attempt can learn from previous failures.
// If maxAttempts is reached, the full history of PreviousAttempts is
// included in the escalation child proposal.
type PreviousAttempt struct {
	// attempt is the 1-based attempt number that failed.
	// +required
	// +kubebuilder:validation:Minimum=1
	Attempt int32 `json:"attempt,omitempty"`
	// failedStep is which workflow step failed (analysis, execution, or verification).
	// +optional
	FailedStep SandboxStep `json:"failedStep,omitempty"`
	// failureReason is the error message or explanation from the failed step.
	// Omit when no failure has occurred; an empty string is treated the same
	// as omitted (no failure). Maximum 8192 characters.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=8192
	FailureReason string `json:"failureReason,omitempty"`
}

// ProposalSpec defines the desired state of Proposal.
//
// A Proposal either references a ProposalTemplate (via templateRef) or
// defines the workflow shape inline. Either templateRef or inline analysis
// must be provided, but not both.
//
// +kubebuilder:validation:XValidation:rule="has(self.templateRef) || has(self.analysis)",message="either templateRef or inline analysis must be provided"
// +kubebuilder:validation:XValidation:rule="!(has(self.templateRef) && has(self.analysis) && has(self.analysis.agent) && self.analysis.agent != '')",message="templateRef and inline analysis.agent are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.templateRef) && has(self.execution) && has(self.execution.agent) && self.execution.agent != '')",message="templateRef and inline execution.agent are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.templateRef) && has(self.verification) && has(self.verification.agent) && self.verification.agent != '')",message="templateRef and inline verification.agent are mutually exclusive"
type ProposalSpec struct {
	// request is the user's original request, alert description, or a
	// description of what triggered this proposal. This text is passed to
	// the analysis agent as the primary input.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32768
	Request string `json:"request,omitempty"`

	// templateRef references a cluster-scoped ProposalTemplate that
	// defines the workflow shape (which steps run, which agent tier
	// handles each step).
	// +optional
	TemplateRef *ProposalTemplateReference `json:"templateRef,omitempty"`

	// targetNamespaces are the Kubernetes namespace(s) this proposal
	// operates on. Used for RBAC scoping and context to the analysis agent.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +kubebuilder:validation:XValidation:rule="self.all(ns, !format.dns1123Label().validate(ns).hasValue())",message="each namespace must be a valid DNS label"
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=63
	TargetNamespaces []string `json:"targetNamespaces,omitempty"`

	// tools defines the default tools for all steps: skills images,
	// required secrets, and output schema. Per-step tools
	// (analysis.tools, execution.tools, verification.tools) replace
	// this default for individual steps.
	// +optional
	Tools ToolsSpec `json:"tools,omitzero"`

	// analysis defines per-step configuration for the analysis step.
	// When templateRef is not set, analysis.agent selects the agent tier
	// (inline workflow definition). When templateRef is set, only
	// analysis.tools is meaningful (per-step tools override).
	// +optional
	Analysis *ProposalStep `json:"analysis,omitempty"`

	// execution defines per-step configuration for the execution step.
	// Only execution.tools is meaningful (per-step tools override).
	// +optional
	Execution *ProposalStep `json:"execution,omitempty"`

	// verification defines per-step configuration for the verification step.
	// Only verification.tools is meaningful (per-step tools override).
	// +optional
	Verification *ProposalStep `json:"verification,omitempty"`

	// parent references the parent proposal in an escalation chain.
	// Set automatically by the operator when creating a child proposal.
	// +optional
	Parent ProposalReference `json:"parent,omitzero"`

	// maxAttempts overrides the template's retry limit for this proposal.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=20
	MaxAttempts *int32 `json:"maxAttempts,omitempty"`

	// revision is incremented by the user (or console UI) each time they
	// submit revision feedback for the analysis.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Revision *int32 `json:"revision,omitempty"`
}

// ProposalStatus defines the observed state of Proposal. All fields are
// set by the operator -- users should not modify status fields directly.
// The status provides complete observability into the proposal's progress,
// including per-step results, retry history, and standard Kubernetes conditions.
type ProposalStatus struct {
	// conditions represent the latest available observations using the
	// standard Kubernetes condition pattern. Condition types include:
	// Analyzed, Approved, Executed, Verified, and Escalated.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// phase summarizes the proposal's lifecycle state for display purposes.
	// Derived by the operator from conditions on every reconcile. Conditions
	// are the source of truth; this field is for human consumption
	// (oc get, console list views).
	// +optional
	Phase ProposalPhase `json:"phase,omitempty"` //nolint:kubeapilinter // Phase is derived from conditions for display (oc get, console).

	// attempt is the current attempt number (1-based). Incremented each
	// time the proposal is retried after a failure. Starts at 1 for the
	// first attempt.
	// +optional
	Attempt *int32 `json:"attempt,omitempty"`

	// steps contains the per-step observed state (analysis, execution,
	// verification). Each step independently tracks its timing, sandbox
	// info, and results.
	// +optional
	Steps StepsStatus `json:"steps,omitzero"`

	// previousAttempts contains the failure history from earlier attempts.
	// Each entry records which step failed and why, giving the analysis
	// agent on the next attempt context to avoid repeating the same mistake.
	// Maximum 20 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	PreviousAttempts []PreviousAttempt `json:"previousAttempts,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.templateRef.name`
// +kubebuilder:printcolumn:name="Request",type=string,JSONPath=`.spec.request`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Proposal represents a unit of work managed by the agentic platform.
// It is the primary resource component teams and adapters interact with.
//
// A Proposal either references a ProposalTemplate (via templateRef) that
// defines the workflow shape, or defines the workflow inline. Tools
// (skills images, required secrets) are specified directly on the Proposal.
//
// Example — ACS remediation using a template:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Proposal
//	metadata:
//	  name: fix-nginx-cve-2024-1234
//	  namespace: stackrox
//	spec:
//	  request: |
//	    ACS violation: Deployment nginx in namespace lightspeed-demo
//	    is running nginx:1.21 which has CVE-2024-1234 (Critical).
//	  templateRef:
//	    name: remediation
//	  targetNamespaces:
//	    - lightspeed-demo
//	  tools:
//	    skills:
//	      - image: registry.redhat.io/acs/acs-lightspeed-skills:latest
//	    requiredSecrets:
//	      - name: acs-api-token
//	        mountAs: ACS_API_TOKEN
//
// Example — fully inline (no template):
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Proposal
//	metadata:
//	  name: one-off-investigation
//	spec:
//	  request: "Investigate why pod foo is crashlooping"
//	  targetNamespaces:
//	    - lightspeed-demo
//	  tools:
//	    skills:
//	      - image: registry.redhat.io/acs/acs-lightspeed-skills:latest
//	  analysis:
//	    agent: smart
type Proposal struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Proposal.
	// +required
	Spec ProposalSpec `json:"spec,omitzero"`

	// status defines the observed state of Proposal.
	// +optional
	Status ProposalStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ProposalList contains a list of Proposal.
type ProposalList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Proposal `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Proposal{}, &ProposalList{})
}
