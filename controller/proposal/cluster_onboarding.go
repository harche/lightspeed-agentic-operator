package proposal

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
)

const (
	msaName                  = "lightspeed"
	spokeSANamespace         = "open-cluster-management-agent-addon"
	kubeconfigSecretName     = "lightspeed-spoke-kubeconfig"
	baseRBACManifestWorkName = "lightspeed-base-rbac"
	onboardingFinalizer      = "agentic.openshift.io/cluster-onboarding"
	onboardingLabelManagedBy = "agentic.openshift.io/managed-by"
	onboardingLabelValue     = "lightspeed-agentic-operator"
)

type ClusterOnboardingReconciler struct {
	client.Client
	Log       logr.Logger
	Namespace string
}

func (r *ClusterOnboardingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.ManagedCluster{}).
		Named("cluster-onboarding").
		Complete(r)
}

func (r *ClusterOnboardingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("cluster", req.Name)

	mc := &clusterv1.ManagedCluster{}
	if err := r.Get(ctx, req.NamespacedName, mc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !mc.DeletionTimestamp.IsZero() {
		if controllerutil.RemoveFinalizer(mc, onboardingFinalizer) {
			if err := r.Update(ctx, mc); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !isClusterAvailable(mc) {
		log.Info("cluster not available, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if controllerutil.AddFinalizer(mc, onboardingFinalizer) {
		if err := r.Update(ctx, mc); err != nil {
			return ctrl.Result{}, err
		}
	}

	clusterName := mc.Name
	apiServerURL := ""
	if len(mc.Spec.ManagedClusterClientConfigs) > 0 {
		apiServerURL = mc.Spec.ManagedClusterClientConfigs[0].URL
	}
	if apiServerURL == "" {
		log.Info("no API server URL found, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if err := r.ensureMSA(ctx, clusterName); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring MSA: %w", err)
	}

	if err := r.ensureBaseRBAC(ctx, clusterName); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring base RBAC: %w", err)
	}

	msa := &msav1beta1.ManagedServiceAccount{}
	if err := r.Get(ctx, types.NamespacedName{Name: msaName, Namespace: clusterName}, msa); err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if !isMSATokenReady(msa) {
		log.Info("MSA token not ready, requeuing")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	tokenSecretName := msa.Status.TokenSecretRef.Name
	tokenSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: tokenSecretName, Namespace: clusterName}, tokenSecret); err != nil {
		log.Info("MSA token secret not found, requeuing", "secret", tokenSecretName)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	token := string(tokenSecret.Data["token"])
	caCert := tokenSecret.Data["ca.crt"]

	kubeconfigData, err := buildMergedKubeconfig(apiServerURL, caCert, token, clusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building kubeconfig: %w", err)
	}

	if err := r.ensureKubeconfigSecret(ctx, clusterName, r.Namespace, kubeconfigData); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring kubeconfig secret: %w", err)
	}

	log.Info("onboarding complete", "apiServer", apiServerURL)
	return ctrl.Result{}, nil
}

func (r *ClusterOnboardingReconciler) ensureMSA(ctx context.Context, clusterName string) error {
	msa := &msav1beta1.ManagedServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      msaName,
			Namespace: clusterName,
			Labels: map[string]string{
				onboardingLabelManagedBy: onboardingLabelValue,
			},
		},
		Spec: msav1beta1.ManagedServiceAccountSpec{
			Rotation: msav1beta1.ManagedServiceAccountRotation{
				Enabled:  true,
				Validity: metav1.Duration{Duration: 720 * time.Hour},
			},
		},
	}
	err := r.Create(ctx, msa)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (r *ClusterOnboardingReconciler) ensureBaseRBAC(ctx context.Context, clusterName string) error {
	mw := buildBaseRBACManifestWork(clusterName)
	err := r.Create(ctx, mw)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func spokeKubeconfigSecretName(clusterName string) string {
	return truncateK8sName(kubeconfigSecretName + "-" + clusterName)
}

func (r *ClusterOnboardingReconciler) ensureKubeconfigSecret(ctx context.Context, clusterName, operatorNS string, kubeconfig []byte) error {
	secretName := spokeKubeconfigSecretName(clusterName)
	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: secretName, Namespace: operatorNS}
	err := r.Get(ctx, key, secret)
	if errors.IsNotFound(err) {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: operatorNS,
				Labels: map[string]string{
					onboardingLabelManagedBy: onboardingLabelValue,
					"agentic.openshift.io/target-cluster": clusterName,
				},
			},
			Data: map[string][]byte{
				"kubeconfig": kubeconfig,
			},
		}
		return r.Create(ctx, secret)
	}
	if err != nil {
		return err
	}
	secret.Data = map[string][]byte{"kubeconfig": kubeconfig}
	return r.Update(ctx, secret)
}

// buildMergedKubeconfig creates a kubeconfig with two contexts:
//   - spoke context (default): uses the MSA token to access the spoke cluster
//   - hub context: uses tokenFile to reference the pod's auto-mounted SA token
//
// The agent can run `kubectl` (targets spoke by default) or
// `kubectl --context=hub` to access the hub (e.g., Thanos).
func buildMergedKubeconfig(spokeAPIServerURL string, spokeCACert []byte, spokeToken string, clusterName string) ([]byte, error) {
	config := clientcmdapi.NewConfig()

	config.Clusters[clusterName] = &clientcmdapi.Cluster{
		Server:                   spokeAPIServerURL,
		CertificateAuthorityData: spokeCACert,
	}
	config.AuthInfos[clusterName] = &clientcmdapi.AuthInfo{
		Token: spokeToken,
	}
	config.Contexts[clusterName] = &clientcmdapi.Context{
		Cluster:  clusterName,
		AuthInfo: clusterName,
	}

	config.Clusters["hub"] = &clientcmdapi.Cluster{
		Server:               "https://kubernetes.default.svc",
		CertificateAuthority: "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
	}
	config.AuthInfos["hub"] = &clientcmdapi.AuthInfo{
		TokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
	}
	config.Contexts["hub"] = &clientcmdapi.Context{
		Cluster:  "hub",
		AuthInfo: "hub",
	}

	config.CurrentContext = clusterName

	data, err := clientcmd.Write(*config)
	if err != nil {
		return nil, fmt.Errorf("serializing kubeconfig: %w", err)
	}
	return data, nil
}

func isClusterAvailable(mc *clusterv1.ManagedCluster) bool {
	for _, c := range mc.Status.Conditions {
		if c.Type == "ManagedClusterConditionAvailable" && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func isMSATokenReady(msa *msav1beta1.ManagedServiceAccount) bool {
	if msa.Status.TokenSecretRef == nil {
		return false
	}
	for _, c := range msa.Status.Conditions {
		if c.Type == "TokenReported" && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

var (
	_ = &workv1.ManifestWork{}
	_ = &msav1beta1.ManagedServiceAccount{}
)
