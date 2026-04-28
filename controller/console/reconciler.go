package console

import (
	"context"
	"fmt"
	"slices"

	consolev1 "github.com/openshift/api/console/v1"
	openshiftv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	pluginName    = "lightspeed-agentic-console-plugin"
	pluginPort    = 9443
	certSecretName = pluginName + "-cert"
	consoleCRName = "cluster"

	servingCertAnnotation = "service.beta.openshift.io/serving-cert-secret-name"

	nginxConfig = `error_log /dev/stdout info;
events {}
http {
  include            /etc/nginx/mime.types;
  default_type       application/octet-stream;
  keepalive_timeout  65;
  server {
    listen              9443 ssl;
    ssl_certificate     /var/cert/tls.crt;
    ssl_certificate_key /var/cert/tls.key;
    root                /usr/share/nginx/html;
    access_log          /dev/stdout;
  }
}
`
)

type AgenticConsoleConfig struct {
	Image     string
	Namespace string
}

func EnsureAgenticConsole(ctx context.Context, c client.Client, cfg AgenticConsoleConfig) error {
	log := logf.FromContext(ctx).WithName("agentic-console")

	if cfg.Image == "" {
		log.Info("No agentic console image configured — skipping console plugin deployment")
		return nil
	}

	log.Info("Ensuring agentic console plugin", "image", cfg.Image, "namespace", cfg.Namespace)

	for _, fn := range []struct {
		name string
		fn   func(context.Context, client.Client, AgenticConsoleConfig) error
	}{
		{"ConfigMap", ensureConfigMap},
		{"ServiceAccount", ensureServiceAccount},
		{"Service", ensureService},
		{"Deployment", ensureDeployment},
		{"ConsolePlugin", ensureConsolePlugin},
		{"ConsoleActivation", ensureConsoleActivation},
	} {
		if err := fn.fn(ctx, c, cfg); err != nil {
			return fmt.Errorf("ensure %s: %w", fn.name, err)
		}
		log.V(1).Info("Resource ready", "resource", fn.name)
	}

	log.Info("Agentic console plugin deployed")
	return nil
}

func labels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       pluginName,
		"app.kubernetes.io/component":  "console",
		"app.kubernetes.io/managed-by": "lightspeed-operator",
	}
}

func ensureConfigMap(ctx context.Context, c client.Client, cfg AgenticConsoleConfig) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: pluginName, Namespace: cfg.Namespace, Labels: labels()},
		Data:       map[string]string{"nginx.conf": nginxConfig},
	}
	return createOrUpdate(ctx, c, cm, func(existing *corev1.ConfigMap) {
		existing.Data = cm.Data
	})
}

func ensureServiceAccount(ctx context.Context, c client.Client, cfg AgenticConsoleConfig) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: pluginName, Namespace: cfg.Namespace, Labels: labels()},
	}
	return createIfNotExists(ctx, c, sa)
}

func ensureService(ctx context.Context, c client.Client, cfg AgenticConsoleConfig) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pluginName,
			Namespace: cfg.Namespace,
			Labels:    labels(),
			Annotations: map[string]string{
				servingCertAnnotation: certSecretName,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app.kubernetes.io/name": pluginName},
			Ports: []corev1.ServicePort{{
				Name:       "https",
				Port:       pluginPort,
				TargetPort: intstr.FromInt32(pluginPort),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
	return createOrUpdate(ctx, c, svc, func(existing *corev1.Service) {
		existing.Annotations = svc.Annotations
		existing.Spec.Ports = svc.Spec.Ports
		existing.Spec.Selector = svc.Spec.Selector
	})
}

func ensureDeployment(ctx context.Context, c client.Client, cfg AgenticConsoleConfig) error {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: pluginName, Namespace: cfg.Namespace, Labels: labels()},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": pluginName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels()},
				Spec: corev1.PodSpec{
					ServiceAccountName: pluginName,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: boolPtr(true),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:            "console",
						Image:           cfg.Image,
						ImagePullPolicy: corev1.PullAlways,
						Ports: []corev1.ContainerPort{{
							ContainerPort: pluginPort,
							Protocol:      corev1.ProtocolTCP,
						}},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: boolPtr(false),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("50Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("100Mi"),
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "cert", MountPath: "/var/cert", ReadOnly: true},
							{Name: "nginx-conf", MountPath: "/etc/nginx/nginx.conf", SubPath: "nginx.conf", ReadOnly: true},
							{Name: "nginx-tmp", MountPath: "/tmp/nginx"},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "cert", VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: certSecretName},
						}},
						{Name: "nginx-conf", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: pluginName},
							},
						}},
						{Name: "nginx-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
	return createOrUpdate(ctx, c, dep, func(existing *appsv1.Deployment) {
		existing.Spec.Template.Spec.Containers[0].Image = cfg.Image
		existing.Spec.Template = dep.Spec.Template
	})
}

func ensureConsolePlugin(ctx context.Context, c client.Client, cfg AgenticConsoleConfig) error {
	plugin := &consolev1.ConsolePlugin{
		ObjectMeta: metav1.ObjectMeta{Name: pluginName, Labels: labels()},
		Spec: consolev1.ConsolePluginSpec{
			DisplayName: "OpenShift Lightspeed Agentic Console Plugin",
			Backend: consolev1.ConsolePluginBackend{
				Type: consolev1.Service,
				Service: &consolev1.ConsolePluginService{
					Name:      pluginName,
					Namespace: cfg.Namespace,
					Port:      pluginPort,
					BasePath:  "/",
				},
			},
			I18n: consolev1.ConsolePluginI18n{LoadType: consolev1.Preload},
		},
	}
	return createOrUpdate(ctx, c, plugin, func(existing *consolev1.ConsolePlugin) {
		existing.Spec = plugin.Spec
	})
}

func ensureConsoleActivation(ctx context.Context, c client.Client, _ AgenticConsoleConfig) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		console := &openshiftv1.Console{}
		if err := c.Get(ctx, types.NamespacedName{Name: consoleCRName}, console); err != nil {
			return fmt.Errorf("get Console CR: %w", err)
		}
		if slices.Contains(console.Spec.Plugins, pluginName) {
			return nil
		}
		console.Spec.Plugins = append(console.Spec.Plugins, pluginName)
		return c.Update(ctx, console)
	})
}

// --- helpers ---

type clientObject[T any] interface {
	client.Object
	*T
}

func createIfNotExists[T any, PT clientObject[T]](ctx context.Context, c client.Client, obj PT) error {
	if err := c.Create(ctx, obj); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}

func createOrUpdate[T any, PT clientObject[T]](ctx context.Context, c client.Client, desired PT, update func(*T)) error {
	existing := PT(new(T))
	err := c.Get(ctx, types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			return c.Create(ctx, desired)
		}
		return err
	}
	update(existing)
	return c.Update(ctx, existing)
}

func boolPtr(b bool) *bool { return &b }
