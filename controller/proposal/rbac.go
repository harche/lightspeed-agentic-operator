package proposal

import (
	"context"
	"fmt"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	rbacNamespacesAnnotation    = "agentic.openshift.io/rbac-namespaces"
	rbacTargetClusterAnnotation = "agentic.openshift.io/rbac-target-cluster"
)

// ensureExecutionRBAC creates Role+RoleBinding (namespace-scoped) and
// ClusterRole+ClusterRoleBinding (cluster-scoped) from the selected
// option's RBAC result. When targetCluster is set on the proposal,
// RBAC is created on the spoke cluster via a kubeconfig-based client
// instead of locally. Idempotent — skips resources that already exist.
func ensureExecutionRBAC(
	ctx context.Context,
	c client.Client,
	proposal *agenticv1alpha1.Proposal,
	rbacResult *agenticv1alpha1.RBACResult,
	sandboxSA string,
	operatorNS string,
) error {
	if rbacResult == nil {
		return nil
	}

	targetCluster := proposal.Spec.TargetCluster
	if targetCluster != "" {
		return ensureExecutionRBACOnSpoke(ctx, c, proposal, rbacResult, targetCluster)
	}

	return ensureExecutionRBACLocal(ctx, c, proposal, rbacResult, sandboxSA, operatorNS)
}

func ensureExecutionRBACLocal(
	ctx context.Context,
	c client.Client,
	proposal *agenticv1alpha1.Proposal,
	rbacResult *agenticv1alpha1.RBACResult,
	sandboxSA string,
	operatorNS string,
) error {
	roleName := executionRoleName(proposal.Name)
	labels := rbacLabels(proposal.Name, "execution-rbac")

	subjects := []rbacv1.Subject{{
		Kind:      rbacv1.ServiceAccountKind,
		Name:      sandboxSA,
		Namespace: operatorNS,
	}}

	if len(rbacResult.NamespaceScoped) > 0 {
		nsRules := rbacRulesToPolicyRules(rbacResult.NamespaceScoped)
		targetNS := rbacTargetNamespaces(proposal, rbacResult)

		if len(targetNS) > 0 {
			if proposal.Annotations == nil {
				proposal.Annotations = make(map[string]string)
			}
			proposal.Annotations[rbacNamespacesAnnotation] = strings.Join(targetNS, ",")
		}

		for _, ns := range targetNS {
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: ns, Labels: labels},
				Rules:      nsRules,
			}
			if err := c.Create(ctx, role); err != nil && !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("create Role in %s: %w", ns, err)
			}
			binding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: ns, Labels: labels},
				RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: roleName},
				Subjects:   subjects,
			}
			if err := c.Create(ctx, binding); err != nil && !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("create RoleBinding in %s: %w", ns, err)
			}
		}
	}

	if len(rbacResult.ClusterScoped) > 0 {
		crName := clusterRoleName(proposal.Name)
		clusterRules := rbacRulesToPolicyRules(rbacResult.ClusterScoped)
		cr := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: crName, Labels: labels},
			Rules:      clusterRules,
		}
		if err := c.Create(ctx, cr); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create ClusterRole %s: %w", crName, err)
		}
		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: crName, Labels: labels},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: crName},
			Subjects:   subjects,
		}
		if err := c.Create(ctx, crb); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create ClusterRoleBinding %s: %w", crName, err)
		}
	}

	return nil
}

func ensureExecutionRBACOnSpoke(
	ctx context.Context,
	hubClient client.Client,
	proposal *agenticv1alpha1.Proposal,
	rbacResult *agenticv1alpha1.RBACResult,
	targetCluster string,
) error {
	if proposal.Annotations == nil {
		proposal.Annotations = make(map[string]string)
	}
	proposal.Annotations[rbacTargetClusterAnnotation] = targetCluster

	targetNS := rbacTargetNamespaces(proposal, rbacResult)
	if len(targetNS) > 0 {
		proposal.Annotations[rbacNamespacesAnnotation] = strings.Join(targetNS, ",")
	}

	mw := buildExecutionRBACManifestWork(proposal.Name, targetCluster, rbacResult, targetNS)
	if len(mw.Spec.Workload.Manifests) == 0 {
		return nil
	}

	if err := hubClient.Create(ctx, mw); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create execution RBAC ManifestWork in %s: %w", targetCluster, err)
	}
	return nil
}

func executionRBACManifestWorkName(proposalName string) string {
	return truncateK8sName("ls-exec-rbac-" + proposalName)
}

func buildExecutionRBACManifestWork(
	proposalName string,
	targetCluster string,
	rbacResult *agenticv1alpha1.RBACResult,
	targetNS []string,
) *workv1.ManifestWork {
	var manifests []workv1.Manifest

	roleName := executionRoleName(proposalName)
	labels := rbacLabels(proposalName, "execution-rbac")

	spokeSubjects := []rbacv1.Subject{{
		Kind:      rbacv1.ServiceAccountKind,
		Name:      msaName,
		Namespace: spokeSANamespace,
	}}

	if len(rbacResult.NamespaceScoped) > 0 {
		nsRules := rbacRulesToPolicyRules(rbacResult.NamespaceScoped)
		for _, ns := range targetNS {
			role := &rbacv1.Role{
				TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "Role"},
				ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: ns, Labels: labels},
				Rules:      nsRules,
			}
			binding := &rbacv1.RoleBinding{
				TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding"},
				ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: ns, Labels: labels},
				RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: roleName},
				Subjects:   spokeSubjects,
			}
			manifests = append(manifests, toManifest(role), toManifest(binding))
		}
	}

	if len(rbacResult.ClusterScoped) > 0 {
		crName := clusterRoleName(proposalName)
		clusterRules := rbacRulesToPolicyRules(rbacResult.ClusterScoped)
		cr := &rbacv1.ClusterRole{
			TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole"},
			ObjectMeta: metav1.ObjectMeta{Name: crName, Labels: labels},
			Rules:      clusterRules,
		}
		crb := &rbacv1.ClusterRoleBinding{
			TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRoleBinding"},
			ObjectMeta: metav1.ObjectMeta{Name: crName, Labels: labels},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: crName},
			Subjects:   spokeSubjects,
		}
		manifests = append(manifests, toManifest(cr), toManifest(crb))
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      executionRBACManifestWorkName(proposalName),
			Namespace: targetCluster,
			Labels:    rbacLabels(proposalName, "execution-rbac"),
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}
}

func toManifest(obj interface{}) workv1.Manifest {
	data := mustMarshalJSON(obj)
	return workv1.Manifest{RawExtension: runtime.RawExtension{Raw: data}}
}

// cleanupExecutionRBAC removes all RBAC resources created for a proposal's
// execution. When the proposal targeted a spoke cluster, RBAC is deleted
// from the spoke via a kubeconfig-based client. Uses annotations to find
// namespaces and target cluster (survives retry clearing Steps).
func cleanupExecutionRBAC(ctx context.Context, c client.Client, proposal *agenticv1alpha1.Proposal) error {
	targetCluster := annotatedTargetCluster(proposal)
	if targetCluster != "" {
		return cleanupExecutionRBACOnSpoke(ctx, c, proposal, targetCluster)
	}

	return cleanupExecutionRBACLocal(ctx, c, proposal)
}

func cleanupExecutionRBACLocal(ctx context.Context, c client.Client, proposal *agenticv1alpha1.Proposal) error {
	roleName := executionRoleName(proposal.Name)

	for _, ns := range annotatedRBACNamespaces(proposal) {
		if err := deleteIfExists(ctx, c, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: ns}}); err != nil {
			return fmt.Errorf("delete RoleBinding in %s: %w", ns, err)
		}
		if err := deleteIfExists(ctx, c, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: ns}}); err != nil {
			return fmt.Errorf("delete Role in %s: %w", ns, err)
		}
	}

	crName := clusterRoleName(proposal.Name)
	if err := deleteIfExists(ctx, c, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crName}}); err != nil {
		return fmt.Errorf("delete ClusterRoleBinding %s: %w", crName, err)
	}
	if err := deleteIfExists(ctx, c, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: crName}}); err != nil {
		return fmt.Errorf("delete ClusterRole %s: %w", crName, err)
	}
	return nil
}

func cleanupExecutionRBACOnSpoke(ctx context.Context, hubClient client.Client, proposal *agenticv1alpha1.Proposal, targetCluster string) error {
	mwName := executionRBACManifestWorkName(proposal.Name)
	mw := &workv1.ManifestWork{}
	key := types.NamespacedName{Name: mwName, Namespace: targetCluster}
	if err := hubClient.Get(ctx, key, mw); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get execution RBAC ManifestWork %s/%s: %w", targetCluster, mwName, err)
	}
	if err := hubClient.Delete(ctx, mw); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete execution RBAC ManifestWork %s/%s: %w", targetCluster, mwName, err)
	}
	return nil
}

// isExecutionRBACAppliedOnSpoke checks whether the execution RBAC ManifestWork
// has been applied on the spoke cluster. Returns true if the ManifestWork
// has condition Applied=True. Returns false if not yet applied or not found.
func isExecutionRBACAppliedOnSpoke(ctx context.Context, hubClient client.Client, proposal *agenticv1alpha1.Proposal, targetCluster string) (bool, error) {
	mwName := executionRBACManifestWorkName(proposal.Name)
	mw := &workv1.ManifestWork{}
	key := types.NamespacedName{Name: mwName, Namespace: targetCluster}
	if err := hubClient.Get(ctx, key, mw); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get ManifestWork %s/%s: %w", targetCluster, mwName, err)
	}

	for _, c := range mw.Status.Conditions {
		if c.Type == workv1.WorkApplied && c.Status == metav1.ConditionTrue {
			return true, nil
		}
	}
	return false, nil
}

func annotatedTargetCluster(proposal *agenticv1alpha1.Proposal) string {
	if proposal.Annotations == nil {
		return ""
	}
	return proposal.Annotations[rbacTargetClusterAnnotation]
}

func annotatedRBACNamespaces(proposal *agenticv1alpha1.Proposal) []string {
	if proposal.Annotations == nil {
		return nil
	}
	val := proposal.Annotations[rbacNamespacesAnnotation]
	if val == "" {
		return nil
	}
	return strings.Split(val, ",")
}

func deleteIfExists(ctx context.Context, c client.Client, obj client.Object) error {
	if err := c.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func rbacTargetNamespaces(proposal *agenticv1alpha1.Proposal, rbacResult *agenticv1alpha1.RBACResult) []string {
	if len(proposal.Spec.TargetNamespaces) > 0 {
		return proposal.Spec.TargetNamespaces
	}
	if rbacResult == nil {
		return nil
	}
	seen := make(map[string]bool)
	var nsList []string
	for _, rule := range rbacResult.NamespaceScoped {
		if rule.Namespace != "" && !seen[rule.Namespace] {
			nsList = append(nsList, rule.Namespace)
			seen[rule.Namespace] = true
		}
	}
	return nsList
}

func truncateK8sName(name string) string {
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	return name
}

func executionRoleName(proposalName string) string {
	return truncateK8sName("ls-exec-" + proposalName)
}

func clusterRoleName(proposalName string) string {
	return truncateK8sName("ls-exec-cluster-" + proposalName)
}

func rbacLabels(proposalName, component string) map[string]string {
	return map[string]string{
		LabelProposal:  proposalName,
		LabelComponent: component,
	}
}

func rbacRulesToPolicyRules(rules []agenticv1alpha1.RBACRule) []rbacv1.PolicyRule {
	out := make([]rbacv1.PolicyRule, len(rules))
	for i, r := range rules {
		out[i] = rbacv1.PolicyRule{
			APIGroups:     normalizeCoreAPIGroup(r.APIGroups),
			Resources:     r.Resources,
			ResourceNames: r.ResourceNames,
			Verbs:         r.Verbs,
		}
	}
	return out
}

// normalizeCoreAPIGroup maps "core" to "" for the Kubernetes core API group.
// The output schema requires minLength=1 so the LLM uses "core" instead of "".
func normalizeCoreAPIGroup(groups []string) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		if g == "core" {
			out[i] = ""
		} else {
			out[i] = g
		}
	}
	return out
}
