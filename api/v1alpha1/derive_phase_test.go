package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func cond(t string, s metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{Type: t, Status: s, Reason: reason}
}

func TestDerivePhase(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       ProposalPhase
	}{
		{
			name:       "no conditions",
			conditions: nil,
			want:       ProposalPhasePending,
		},
		{
			name:       "empty conditions",
			conditions: []metav1.Condition{},
			want:       ProposalPhasePending,
		},
		{
			name: "analyzing",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionUnknown, "AnalysisInProgress"),
			},
			want: ProposalPhaseAnalyzing,
		},
		{
			name: "analysis complete - proposed",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
			},
			want: ProposalPhaseProposed,
		},
		{
			name: "analysis failed",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionFalse, "AnalysisFailed"),
			},
			want: ProposalPhaseFailed,
		},
		{
			name: "approved - awaiting execution",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
			},
			want: ProposalPhaseApproved,
		},
		{
			name: "denied",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionFalse, "UserDenied"),
			},
			want: ProposalPhaseDenied,
		},
		{
			name: "executing",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionExecuted, metav1.ConditionUnknown, "ExecutionInProgress"),
			},
			want: ProposalPhaseExecuting,
		},
		{
			name: "execution failed",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionExecuted, metav1.ConditionFalse, "ExecutionFailed"),
			},
			want: ProposalPhaseFailed,
		},
		{
			name: "execution complete - verifying",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionExecuted, metav1.ConditionTrue, "ExecutionComplete"),
			},
			want: ProposalPhaseVerifying,
		},
		{
			name: "verifying in progress",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionExecuted, metav1.ConditionTrue, "ExecutionComplete"),
				cond(ProposalConditionVerified, metav1.ConditionUnknown, "VerificationInProgress"),
			},
			want: ProposalPhaseVerifying,
		},
		{
			name: "verification passed - completed",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionExecuted, metav1.ConditionTrue, "ExecutionComplete"),
				cond(ProposalConditionVerified, metav1.ConditionTrue, "VerificationPassed"),
			},
			want: ProposalPhaseCompleted,
		},
		{
			name: "verification failed - terminal",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionExecuted, metav1.ConditionTrue, "ExecutionComplete"),
				cond(ProposalConditionVerified, metav1.ConditionFalse, "VerificationFailed"),
			},
			want: ProposalPhaseFailed,
		},
		{
			name: "verification failed - retrying execution",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionVerified, metav1.ConditionFalse, "RetryingExecution"),
			},
			want: ProposalPhaseApproved,
		},
		{
			name: "verification failed - retries exhausted",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionVerified, metav1.ConditionFalse, "RetriesExhausted"),
			},
			want: ProposalPhaseProposed,
		},
		{
			name: "awaiting sync",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionExecuted, metav1.ConditionTrue, "ExecutionSkipped"),
				cond(ProposalConditionAwaitingSync, metav1.ConditionTrue, "AwaitingSync"),
			},
			want: ProposalPhaseAwaitingSync,
		},
		{
			name: "advisory completed - exec and verify skipped",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionExecuted, metav1.ConditionTrue, "ExecutionSkipped"),
				cond(ProposalConditionVerified, metav1.ConditionTrue, "VerificationSkipped"),
			},
			want: ProposalPhaseCompleted,
		},
		{
			name: "escalated",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionEscalated, metav1.ConditionTrue, "MaxAttemptsExhausted"),
			},
			want: ProposalPhaseEscalated,
		},
		{
			name: "escalated takes priority over other conditions",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionTrue, "UserApproved"),
				cond(ProposalConditionExecuted, metav1.ConditionFalse, "ExecutionFailed"),
				cond(ProposalConditionEscalated, metav1.ConditionTrue, "MaxAttemptsExhausted"),
			},
			want: ProposalPhaseEscalated,
		},
		{
			name: "denied takes priority over analyzed",
			conditions: []metav1.Condition{
				cond(ProposalConditionAnalyzed, metav1.ConditionTrue, "AnalysisComplete"),
				cond(ProposalConditionApproved, metav1.ConditionFalse, "UserDenied"),
			},
			want: ProposalPhaseDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DerivePhase(tt.conditions)
			if got != tt.want {
				t.Errorf("DerivePhase() = %q, want %q", got, tt.want)
			}
		})
	}
}
