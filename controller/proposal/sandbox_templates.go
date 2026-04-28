package proposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
)

var sandboxTemplateGVK = schema.GroupVersionKind{
	Group: "extensions.agents.x-k8s.io", Version: "v1alpha1", Kind: "SandboxTemplate",
}

const (
	agentModeEnvVar = "LIGHTSPEED_MODE"

	vertexCredsMountPath = "/var/secrets/google"
	vertexCredsFileName  = "credentials.json"
	llmCredsVolumeName   = "llm-credentials"
	mcpHeadersMountRoot  = "/var/secrets/mcp"
	mcpServersEnvVar     = "LIGHTSPEED_MCP_SERVERS"

	LabelManaged      = "agentic.openshift.io/managed"
	LabelBaseTemplate = "agentic.openshift.io/base-template"
	LabelPhase        = "agentic.openshift.io/phase"
	LabelAgent        = "agentic.openshift.io/agent"
	LabelProposal     = "agentic.openshift.io/proposal"
	LabelComponent    = "agentic.openshift.io/component"
)

type templateHashInput struct {
	LLM            agenticv1alpha1.LLMProviderSpec       `json:"llm"`
	Skills         []agenticv1alpha1.SkillsSource         `json:"skills"`
	MCPServers     []agenticv1alpha1.MCPServerConfig       `json:"mcpServers,omitempty"`
	RequiredSecrets []agenticv1alpha1.SecretRequirement    `json:"requiredSecrets,omitempty"`
	Phase          string                                  `json:"phase"`
}

func computeTemplateHash(
	llm *agenticv1alpha1.LLMProvider,
	skills []agenticv1alpha1.SkillsSource,
	mcpServers []agenticv1alpha1.MCPServerConfig,
	requiredSecrets []agenticv1alpha1.SecretRequirement,
	phase string,
) string {
	input := templateHashInput{
		LLM:             llm.Spec,
		Skills:          skills,
		MCPServers:      mcpServers,
		RequiredSecrets: requiredSecrets,
		Phase:           phase,
	}
	data, err := json.Marshal(input)
	if err != nil {
		data = []byte(fmt.Sprintf("%v", input))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:10]
}

func agentTemplateName(phase, agentName, hash string) string {
	return truncateK8sName(fmt.Sprintf("ls-%s-%s-%s", phase, agentName, hash))
}

// EnsureAgentTemplate creates a SandboxTemplate derived from the base template
// with skills, LLM credentials, MCP servers, and required secrets from the CRD chain.
// Template name includes a config hash — same input = same template = no-op.
// Old templates for the same agent+phase are garbage-collected.
func EnsureAgentTemplate(
	ctx context.Context,
	c client.Client,
	baseTemplateName string,
	namespace string,
	phase string,
	agent *agenticv1alpha1.Agent,
	llm *agenticv1alpha1.LLMProvider,
	ct *agenticv1alpha1.ComponentTools,
) (string, error) {
	log := logf.FromContext(ctx).WithName("sandbox-templates")

	var skills []agenticv1alpha1.SkillsSource
	var mcpServers []agenticv1alpha1.MCPServerConfig
	var requiredSecrets []agenticv1alpha1.SecretRequirement
	if ct != nil {
		skills = ct.Spec.Skills
		mcpServers = ct.Spec.MCPServers
		requiredSecrets = ct.Spec.RequiredSecrets
	}

	hash := computeTemplateHash(llm, skills, mcpServers, requiredSecrets, phase)
	name := agentTemplateName(phase, agent.Name, hash)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(sandboxTemplateGVK)
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, existing)
	if err == nil {
		return name, nil
	}
	if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("failed to check template %q: %w", name, err)
	}

	base := &unstructured.Unstructured{}
	base.SetGroupVersionKind(sandboxTemplateGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: baseTemplateName, Namespace: namespace}, base); err != nil {
		return "", fmt.Errorf("failed to read base sandbox template %q: %w", baseTemplateName, err)
	}

	derived := base.DeepCopy()
	derived.SetName(name)
	derived.SetResourceVersion("")
	derived.SetUID("")
	derived.SetGeneration(0)
	derived.SetCreationTimestamp(metav1.Time{})

	annotations := derived.GetAnnotations()
	if annotations != nil {
		delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		derived.SetAnnotations(annotations)
	}

	lbls := derived.GetLabels()
	if lbls == nil {
		lbls = map[string]string{}
	}
	lbls[LabelManaged] = "true"
	lbls[LabelBaseTemplate] = baseTemplateName
	lbls[LabelPhase] = phase
	lbls[LabelAgent] = agent.Name
	derived.SetLabels(lbls)

	if len(skills) > 0 && skills[0].Image != "" {
		patchSkillsImage(derived, skills[0].Image)
		if len(skills[0].Paths) > 0 {
			patchSkillsPaths(derived, skills[0].Paths)
		}
	}

	patchAgentMode(derived, phase)
	patchLLMCredentials(derived, llm)

	if len(mcpServers) > 0 {
		patchMCPServers(derived, mcpServers)
	}

	if len(requiredSecrets) > 0 {
		patchRequiredSecrets(derived, requiredSecrets)
	}

	if err := c.Create(ctx, derived); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return name, nil
		}
		return "", fmt.Errorf("failed to create template %q: %w", name, err)
	}

	log.Info("Created agent SandboxTemplate",
		"name", name,
		"base", baseTemplateName,
		"phase", phase,
		"agent", agent.Name,
		"llmProvider", llm.Name,
		"hash", hash)

	if err := gcOldTemplates(ctx, c, namespace, agent.Name, phase, name); err != nil {
		log.Error(err, "failed to garbage-collect old templates")
	}

	return name, nil
}

func patchLLMCredentials(tmpl *unstructured.Unstructured, llm *agenticv1alpha1.LLMProvider) {
	secretName := llm.Spec.CredentialsSecret.Name

	addEnvFromSecret(tmpl, secretName)
	setEnvVar(tmpl, "ANTHROPIC_MODEL", llm.Spec.Model)

	if llm.Spec.URL != "" {
		setEnvVar(tmpl, providerURLEnvVar(llm.Spec.Type), llm.Spec.URL)
	}

	switch llm.Spec.Type {
	case agenticv1alpha1.LLMProviderVertex:
		setEnvVar(tmpl, "CLAUDE_CODE_USE_VERTEX", "1")
		setEnvVar(tmpl, "GOOGLE_APPLICATION_CREDENTIALS", vertexCredsMountPath+"/"+vertexCredsFileName)
		addSecretVolume(tmpl, llmCredsVolumeName, secretName)
		addVolumeMount(tmpl, llmCredsVolumeName, vertexCredsMountPath, true)
	case agenticv1alpha1.LLMProviderBedrock:
		setEnvVar(tmpl, "CLAUDE_CODE_USE_BEDROCK", "1")
	}
}

func providerURLEnvVar(t agenticv1alpha1.LLMProviderType) string {
	switch t {
	case agenticv1alpha1.LLMProviderOpenAI:
		return "OPENAI_BASE_URL"
	case agenticv1alpha1.LLMProviderAzureOpenAI:
		return "AZURE_OPENAI_ENDPOINT"
	default:
		return "ANTHROPIC_BASE_URL"
	}
}

func patchRequiredSecrets(tmpl *unstructured.Unstructured, secrets []agenticv1alpha1.SecretRequirement) {
	for _, s := range secrets {
		if strings.HasPrefix(s.MountAs, "/") {
			volName := "req-" + s.Name
			addSecretVolume(tmpl, volName, s.Name)
			addVolumeMount(tmpl, volName, s.MountAs, true)
		} else {
			setEnvVar(tmpl, s.MountAs, "")
			addEnvVarFromSecret(tmpl, s.MountAs, s.Name, "token")
		}
	}
}

func gcOldTemplates(
	ctx context.Context,
	c client.Client,
	namespace string,
	agentName string,
	phase string,
	currentName string,
) error {
	sel := labels.SelectorFromSet(labels.Set{
		LabelManaged: "true",
		LabelAgent:   agentName,
		LabelPhase:   phase,
	})

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(sandboxTemplateGVK)
	if err := c.List(ctx, list, &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: sel,
	}); err != nil {
		return fmt.Errorf("failed to list old templates: %w", err)
	}

	log := logf.FromContext(ctx).WithName("sandbox-templates")
	for i := range list.Items {
		item := &list.Items[i]
		if item.GetName() == currentName {
			continue
		}
		if err := c.Delete(ctx, item); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "failed to delete old template", "name", item.GetName())
			continue
		}
		log.Info("Garbage-collected old SandboxTemplate", "name", item.GetName())
	}
	return nil
}

// SandboxTemplateServiceAccount reads the service account name from a SandboxTemplate.
func SandboxTemplateServiceAccount(ctx context.Context, c client.Client, templateName, namespace string) (string, error) {
	tmpl := &unstructured.Unstructured{}
	tmpl.SetGroupVersionKind(sandboxTemplateGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: templateName, Namespace: namespace}, tmpl); err != nil {
		return "", err
	}
	sa, _, _ := unstructured.NestedString(tmpl.Object, "spec", "podTemplate", "spec", "serviceAccountName")
	return sa, nil
}

// --- Unstructured patch helpers ---

func setEnvVar(tmpl *unstructured.Unstructured, name, value string) {
	upsertEnv(tmpl, name, map[string]any{
		"name":  name,
		"value": value,
	})
}

func addEnvVarFromSecret(tmpl *unstructured.Unstructured, envName, secretName, key string) {
	upsertEnv(tmpl, envName, map[string]any{
		"name": envName,
		"valueFrom": map[string]any{
			"secretKeyRef": map[string]any{
				"name":     secretName,
				"key":      key,
				"optional": true,
			},
		},
	})
}

func addEnvFromSecret(tmpl *unstructured.Unstructured, secretName string) {
	containers, found, _ := unstructured.NestedSlice(tmpl.Object, "spec", "podTemplate", "spec", "containers")
	if !found || len(containers) == 0 {
		return
	}
	container, ok := containers[0].(map[string]any)
	if !ok {
		return
	}
	envFromList, _, _ := unstructured.NestedSlice(container, "envFrom")
	for _, e := range envFromList {
		entry, eOK := e.(map[string]any)
		if !eOK {
			continue
		}
		ref, _ := entry["secretRef"].(map[string]any)
		if ref != nil && ref["name"] == secretName {
			return
		}
	}
	envFromList = append(envFromList, map[string]any{
		"secretRef": map[string]any{
			"name": secretName,
		},
	})
	_ = unstructured.SetNestedSlice(container, envFromList, "envFrom")
	containers[0] = container
	_ = unstructured.SetNestedSlice(tmpl.Object, containers, "spec", "podTemplate", "spec", "containers")
}

func upsertEnv(tmpl *unstructured.Unstructured, name string, entry map[string]any) {
	containers, found, _ := unstructured.NestedSlice(tmpl.Object, "spec", "podTemplate", "spec", "containers")
	if !found || len(containers) == 0 {
		return
	}
	container, ok := containers[0].(map[string]any)
	if !ok {
		return
	}
	envList, _, _ := unstructured.NestedSlice(container, "env")

	updated := false
	for i, e := range envList {
		env, eOK := e.(map[string]any)
		if !eOK {
			continue
		}
		if env["name"] == name {
			envList[i] = entry
			updated = true
			break
		}
	}
	if !updated {
		envList = append(envList, entry)
	}

	_ = unstructured.SetNestedSlice(container, envList, "env")
	containers[0] = container
	_ = unstructured.SetNestedSlice(tmpl.Object, containers, "spec", "podTemplate", "spec", "containers")
}

func addSecretVolume(tmpl *unstructured.Unstructured, volumeName, secretName string) {
	volumes, _, _ := unstructured.NestedSlice(tmpl.Object, "spec", "podTemplate", "spec", "volumes")
	vol := map[string]any{
		"name": volumeName,
		"secret": map[string]any{
			"secretName": secretName,
		},
	}
	for i, v := range volumes {
		existing, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if existing["name"] == volumeName {
			volumes[i] = vol
			_ = unstructured.SetNestedSlice(tmpl.Object, volumes, "spec", "podTemplate", "spec", "volumes")
			return
		}
	}
	volumes = append(volumes, vol)
	_ = unstructured.SetNestedSlice(tmpl.Object, volumes, "spec", "podTemplate", "spec", "volumes")
}

func addVolumeMount(tmpl *unstructured.Unstructured, name, mountPath string, readOnly bool) {
	containers, found, _ := unstructured.NestedSlice(tmpl.Object, "spec", "podTemplate", "spec", "containers")
	if !found || len(containers) == 0 {
		return
	}
	container, ok := containers[0].(map[string]any)
	if !ok {
		return
	}
	mounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")
	mount := map[string]any{
		"name":      name,
		"mountPath": mountPath,
		"readOnly":  readOnly,
	}
	for i, m := range mounts {
		existing, mOK := m.(map[string]any)
		if !mOK {
			continue
		}
		if existing["mountPath"] == mountPath {
			mounts[i] = mount
			_ = unstructured.SetNestedSlice(container, mounts, "volumeMounts")
			containers[0] = container
			_ = unstructured.SetNestedSlice(tmpl.Object, containers, "spec", "podTemplate", "spec", "containers")
			return
		}
	}
	mounts = append(mounts, mount)
	_ = unstructured.SetNestedSlice(container, mounts, "volumeMounts")
	containers[0] = container
	_ = unstructured.SetNestedSlice(tmpl.Object, containers, "spec", "podTemplate", "spec", "containers")
}

func patchSkillsImage(tmpl *unstructured.Unstructured, image string) {
	volumes, found, _ := unstructured.NestedSlice(tmpl.Object, "spec", "podTemplate", "spec", "volumes")
	if !found {
		return
	}
	for i, v := range volumes {
		vol, ok := v.(map[string]any)
		if !ok {
			continue
		}
		volName, _, _ := unstructured.NestedString(vol, "name")
		if volName != "skills" {
			continue
		}
		_ = unstructured.SetNestedField(vol, image, "image", "reference")
		_ = unstructured.SetNestedField(vol, "Always", "image", "pullPolicy")
		volumes[i] = vol
	}
	_ = unstructured.SetNestedSlice(tmpl.Object, volumes, "spec", "podTemplate", "spec", "volumes")
}

func patchSkillsPaths(tmpl *unstructured.Unstructured, paths []string) {
	if len(paths) == 0 {
		return
	}
	containers, found, _ := unstructured.NestedSlice(tmpl.Object, "spec", "podTemplate", "spec", "containers")
	if !found || len(containers) == 0 {
		return
	}
	container, ok := containers[0].(map[string]any)
	if !ok {
		return
	}
	mounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")

	baseMountPath := "/app/skills"
	var filtered []any
	for _, m := range mounts {
		mount, mOK := m.(map[string]any)
		if !mOK {
			filtered = append(filtered, m)
			continue
		}
		if mount["name"] == "skills" {
			if mp, ok := mount["mountPath"].(string); ok {
				baseMountPath = mp
			}
			continue
		}
		filtered = append(filtered, m)
	}

	for _, p := range paths {
		subPath := strings.TrimPrefix(p, "/")
		skillName := path.Base(p)
		mountPath := path.Join(baseMountPath, skillName)
		filtered = append(filtered, map[string]any{
			"name":      "skills",
			"mountPath": mountPath,
			"subPath":   subPath,
			"readOnly":  true,
		})
	}

	_ = unstructured.SetNestedSlice(container, filtered, "volumeMounts")
	containers[0] = container
	_ = unstructured.SetNestedSlice(tmpl.Object, containers, "spec", "podTemplate", "spec", "containers")
}

func patchAgentMode(tmpl *unstructured.Unstructured, mode string) {
	setEnvVar(tmpl, agentModeEnvVar, mode)
}

// --- MCP Server patching ---

type mcpServerEnvEntry struct {
	Name    string              `json:"name"`
	URL     string              `json:"url"`
	Timeout int32               `json:"timeout,omitempty"`
	Headers []mcpHeaderEnvEntry `json:"headers,omitempty"`
}

type mcpHeaderEnvEntry struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	SecretName string `json:"secretName,omitempty"`
}

func patchMCPServers(tmpl *unstructured.Unstructured, servers []agenticv1alpha1.MCPServerConfig) {
	entries := make([]mcpServerEnvEntry, 0, len(servers))
	for _, s := range servers {
		entry := mcpServerEnvEntry{
			Name:    s.Name,
			URL:     s.URL,
			Timeout: s.TimeoutSeconds,
		}
		for _, h := range s.Headers {
			he := mcpHeaderEnvEntry{
				Name:   h.Name,
				Source: string(h.ValueFrom.Type),
			}
			if h.ValueFrom.Type == agenticv1alpha1.MCPHeaderSourceTypeSecret {
				he.SecretName = h.ValueFrom.Secret.Name
				addSecretVolume(tmpl, "mcp-header-"+h.ValueFrom.Secret.Name, h.ValueFrom.Secret.Name)
				addVolumeMount(tmpl, "mcp-header-"+h.ValueFrom.Secret.Name, mcpHeadersMountRoot+"/"+h.ValueFrom.Secret.Name, true)
			}
			entry.Headers = append(entry.Headers, he)
		}
		entries = append(entries, entry)
	}

	data, err := json.Marshal(entries)
	if err != nil {
		logf.Log.Error(err, "failed to marshal MCP server config for env var")
		return
	}
	setEnvVar(tmpl, mcpServersEnvVar, string(data))
}
