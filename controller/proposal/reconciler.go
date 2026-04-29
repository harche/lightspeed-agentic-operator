package proposal

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

// ProposalReconciler reconciles Proposal objects.
//
// Agent must be set before calling SetupWithManager.
type ProposalReconciler struct {
	client.Client
	Log   logr.Logger
	Agent AgentCaller
}

// +kubebuilder:rbac:groups=agentic.openshift.io,resources=proposals,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agentic.openshift.io,resources=proposals/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agentic.openshift.io,resources=proposals/finalizers,verbs=update
// +kubebuilder:rbac:groups=agentic.openshift.io,resources=proposaltemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=agentic.openshift.io,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups=agentic.openshift.io,resources=llmproviders,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;create;delete

func (r *ProposalReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("proposal", req.NamespacedName)

	var proposal agenticv1alpha1.Proposal
	if err := r.Get(ctx, req.NamespacedName, &proposal); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// --- Deletion ---
	if !proposal.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&proposal, proposalFinalizer) {
			if err := r.Agent.ReleaseSandboxes(ctx, &proposal); err != nil {
				log.Error(err, "sandbox cleanup failed during deletion")
			}
			if err := cleanupExecutionRBAC(ctx, r.Client, &proposal); err != nil {
				log.Error(err, "RBAC cleanup failed, retrying")
				return ctrl.Result{}, err
			}
			original := proposal.DeepCopy()
			controllerutil.RemoveFinalizer(&proposal, proposalFinalizer)
			if err := r.Patch(ctx, &proposal, client.MergeFrom(original)); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// --- Initialize status on first reconcile ---
	needsInit := proposal.Status.Phase == "" || proposal.Status.Attempt == nil
	if needsInit {
		base := proposal.DeepCopy()
		if proposal.Status.Phase == "" {
			proposal.Status.Phase = agenticv1alpha1.ProposalPhasePending
		}
		if proposal.Status.Attempt == nil {
			one := int32(1)
			proposal.Status.Attempt = &one
		}
		if err := r.Status().Patch(ctx, &proposal, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, fmt.Errorf("initialize status: %w", err)
		}
	}

	// --- Finalizer ---
	if !controllerutil.ContainsFinalizer(&proposal, proposalFinalizer) {
		if !isTerminal(proposal.Status.Phase) {
			original := proposal.DeepCopy()
			controllerutil.AddFinalizer(&proposal, proposalFinalizer)
			if err := r.Patch(ctx, &proposal, client.MergeFrom(original)); err != nil {
				return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
			}
			// Re-read after metadata patch to get consistent state
			if err := r.Get(ctx, req.NamespacedName, &proposal); err != nil {
				return ctrl.Result{}, client.IgnoreNotFound(err)
			}
		}
	}

	log.Info("reconciling", "phase", proposal.Status.Phase, "attempt", *proposal.Status.Attempt)

	// --- Phase routing ---
	switch proposal.Status.Phase {
	case agenticv1alpha1.ProposalPhaseCompleted,
		agenticv1alpha1.ProposalPhaseDenied,
		agenticv1alpha1.ProposalPhaseAwaitingSync:
		if hasSandboxClaims(&proposal) {
			if err := r.Agent.ReleaseSandboxes(ctx, &proposal); err != nil {
				log.Error(err, "sandbox cleanup failed at terminal phase")
			}
		}
		return ctrl.Result{}, nil

	case agenticv1alpha1.ProposalPhaseProposed:
		if !needsRevision(&proposal) {
			return ctrl.Result{}, nil
		}

	case agenticv1alpha1.ProposalPhaseFailed:
		return r.handleFailed(ctx, log, &proposal)

	case agenticv1alpha1.ProposalPhaseEscalated:
		return r.handleEscalated(ctx, log, &proposal)
	}

	resolved, err := resolveProposal(ctx, r.Client, &proposal)
	if err != nil {
		log.Error(err, "workflow resolution failed")
		base := proposal.DeepCopy()
		proposal.Status.Phase = agenticv1alpha1.ProposalPhaseFailed
		meta.SetStatusCondition(&proposal.Status.Conditions, metav1.Condition{
			Type:    agenticv1alpha1.ProposalConditionAnalyzed,
			Status:  metav1.ConditionFalse,
			Reason:  reasonWorkflowFailed,
			Message: err.Error(),
		})
		if statusErr := r.statusPatch(ctx, &proposal, base); statusErr != nil {
			log.Error(statusErr, "failed to patch status after workflow resolution failure")
		}
		return ctrl.Result{Requeue: true}, nil
	}

	switch proposal.Status.Phase {
	case agenticv1alpha1.ProposalPhasePending, agenticv1alpha1.ProposalPhaseAnalyzing:
		return r.handlePending(ctx, log, &proposal, resolved)

	case agenticv1alpha1.ProposalPhaseProposed:
		return r.handleRevision(ctx, log, &proposal, resolved)

	case agenticv1alpha1.ProposalPhaseApproved, agenticv1alpha1.ProposalPhaseExecuting:
		return r.handleApproved(ctx, log, &proposal, resolved)

	case agenticv1alpha1.ProposalPhaseVerifying:
		return r.handleVerifying(ctx, log, &proposal, resolved)

	default:
		log.Info("unhandled phase, no-op", "phase", proposal.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProposalReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agenticv1alpha1.Proposal{}).
		Named("proposal").
		Complete(r)
}
