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
	corev1 "k8s.io/api/core/v1"
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

// WorkflowOverride allows per-proposal overrides of the referenced workflow.
// This is useful for swapping in a specialized agent for a specific proposal
// without creating a new Workflow CR.
//
// Example — use a specialized ACS analyzer agent for one proposal:
//
//	workflowOverride:
//	  analysis:
//	    name: acs-analyzer
type WorkflowOverride struct {
	// analysis overrides the agent for the analysis step.
	// +optional
	Analysis *corev1.LocalObjectReference `json:"analysis,omitempty"`
	// execution overrides the agent for the execution step.
	// +optional
	Execution *corev1.LocalObjectReference `json:"execution,omitempty"`
	// verification overrides the agent for the verification step.
	// +optional
	Verification *corev1.LocalObjectReference `json:"verification,omitempty"`
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
	FailedStep *SandboxStep `json:"failedStep,omitempty"`
	// failureReason is the error message or explanation from the failed step.
	// Maximum 8192 characters.
	// +optional
	// +kubebuilder:validation:MaxLength=8192
	FailureReason *string `json:"failureReason,omitempty"`
}

// ProposalSpec defines the desired state of Proposal. This is the user-facing
// (or adapter-facing) configuration -- everything the operator needs to start
// processing the proposal.
// +kubebuilder:validation:XValidation:rule="self.workflowRef.name != ''",message="workflowRef.name must not be empty"
// +kubebuilder:validation:XValidation:rule="!has(self.parentRef) || self.parentRef.name != ''",message="parentRef.name must not be empty when set"
type ProposalSpec struct {
	// request references a content resource containing the user's original
	// request, alert description, or a description of what triggered this
	// proposal. The content is served by the aggregated content API and
	// passed to the analysis agent as the primary input. For adapter-created
	// proposals, the adapter creates the content resource with the alert
	// summary and relevant details, then references it here.
	// +required
	Request ContentReference `json:"request,omitzero"`

	// workflowRef references a Workflow CR that defines
	// which agents handle each step (analysis, execution, verification)
	// and which steps are skipped. This is the primary routing mechanism.
	// +required
	WorkflowRef corev1.LocalObjectReference `json:"workflowRef,omitempty"`

	// targetNamespaces are the Kubernetes namespace(s) this proposal
	// operates on. The operator uses these to scope RBAC (creating Roles
	// and RoleBindings only in these namespaces) and to pass context to
	// the analysis agent. When empty, the proposal operates at the
	// cluster level only. Maximum 50 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=50
	TargetNamespaces []string `json:"targetNamespaces,omitempty"`

	// workflowOverride allows per-proposal overrides of the referenced
	// workflow without creating a new Workflow CR. Useful for one-off
	// customizations like skipping execution on a normally full-lifecycle
	// workflow, or swapping in a specialized agent.
	// +optional
	WorkflowOverride *WorkflowOverride `json:"workflowOverride,omitempty"`

	// parentRef references the parent proposal in an escalation chain.
	// Set automatically by the operator when creating a child proposal
	// after maxAttempts is exhausted. The child proposal inherits the
	// full failure history from its parent. The child is also owned by
	// the parent via Kubernetes owner references for garbage collection.
	// +optional
	ParentRef *corev1.LocalObjectReference `json:"parentRef,omitempty"`

	// maxAttempts overrides the global retry limit for this proposal.
	// When a step fails, the operator resets the proposal to Pending
	// with enriched context (up to maxAttempts times). After that, the
	// proposal transitions to Escalated. Set to 0 to disable retries.
	// When omitted, the operator's global default is used. Valid range: 0-20.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=20
	MaxAttempts *int32 `json:"maxAttempts,omitempty"`
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
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// phase summarizes the proposal's lifecycle state for display purposes.
	// Derived by the operator from conditions on every reconcile. Conditions
	// are the source of truth; this field is for human consumption
	// (oc get, console list views).
	// +optional
	Phase ProposalPhase `json:"phase,omitempty"` //nolint:kubeapilinter // Phase is derived from conditions for display (oc get, console).

	// observedGeneration is the most recent generation observed by the
	// operator. It corresponds to the Proposal's generation, which is
	// updated on mutation by the API Server.
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// attempt is the current attempt number (1-based). Incremented each
	// time the proposal is retried after a failure. Starts at 1 for the
	// first attempt.
	// +optional
	Attempt *int32 `json:"attempt,omitempty"`

	// steps contains the per-step observed state (analysis, execution,
	// verification). Each step independently tracks its timing, sandbox
	// info, and results.
	// +optional
	Steps *StepsStatus `json:"steps,omitempty"`

	// previousAttempts contains the failure history from earlier attempts.
	// Each entry records which step failed and why, giving the analysis
	// agent on the next attempt context to avoid repeating the same mistake.
	// Maximum 20 items.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	PreviousAttempts []PreviousAttempt `json:"previousAttempts,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Workflow",type=string,JSONPath=`.spec.workflowRef.name`
// +kubebuilder:printcolumn:name="Request",type=string,JSONPath=`.spec.request`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Proposal represents a unit of work managed by the agentic platform. It is
// the final link in the CRD chain (LLMProvider -> Agent -> Workflow ->
// Proposal) and the primary resource users and adapters interact with.
//
// A Proposal references a Workflow that defines which agents handle each
// step, and tracks the full lifecycle from initial request through analysis,
// user approval, execution, and verification. Proposals are created by
// adapters (AlertManager webhook, ACS violation webhook, manual creation)
// or by the operator itself (escalation child proposals).
//
// Proposal is namespace-scoped for multi-tenancy. The operator watches for new Proposals and
// drives them through the lifecycle automatically. Users interact with
// proposals after analysis to approve, deny, or escalate.
//
// Example — a remediation proposal targeting a specific namespace:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Proposal
//	metadata:
//	  name: fix-crashloop
//	spec:
//	  request:
//	    name: fix-crashloop-request
//	  workflowRef:
//	    name: remediation
//	  targetNamespaces:
//	    - production
//
// Example — use a specialized agent via workflowOverride:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Proposal
//	metadata:
//	  name: acs-fix
//	spec:
//	  request:
//	    name: acs-fix-request
//	  workflowRef:
//	    name: remediation
//	  targetNamespaces:
//	    - production
//	  workflowOverride:
//	    analysis:
//	      name: acs-analyzer
//
// Example — an upgrade proposal with limited retries:
//
//	apiVersion: agentic.openshift.io/v1alpha1
//	kind: Proposal
//	metadata:
//	  name: upgrade-4-22
//	spec:
//	  request:
//	    name: upgrade-4-22-request
//	  workflowRef:
//	    name: upgrade
//	  maxAttempts: 2
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
	Status *ProposalStatus `json:"status,omitempty"`
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
