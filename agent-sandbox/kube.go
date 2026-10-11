package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	claimGVR   = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1beta1", Resource: "sandboxclaims"}
	sandboxGVR = schema.GroupVersionResource{Group: "agents.x-k8s.io", Version: "v1beta1", Resource: "sandboxes"}
)

const (
	managedByLabel = "app.kubernetes.io/managed-by"
	managedBy      = "herdr-sandbox"
	repoAnnotation = "herdr.wibrow.dev/repo"
	refAnnotation  = "herdr.wibrow.dev/ref"
)

type kube struct {
	cfg  config
	rest *rest.Config
	core kubernetes.Interface
	dyn  dynamic.Interface
}

func newKube(cfg config) (*kube, error) {
	rc, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: cfg.Context},
	).ClientConfig()
	if err != nil {
		return nil, err
	}
	core, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(rc)
	if err != nil {
		return nil, err
	}
	return &kube{cfg: cfg, rest: rc, core: core, dyn: dyn}, nil
}

func (k *kube) claims() dynamic.ResourceInterface {
	return k.dyn.Resource(claimGVR).Namespace(k.cfg.Namespace)
}

var nonDNS = regexp.MustCompile(`[^a-z0-9-]+`)

func claimName(repo string) string {
	b := make([]byte, 3)
	rand.Read(b)
	name := repo[strings.LastIndex(repo, "/")+1:]
	name = strings.Trim(nonDNS.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(name) > 40 {
		name = strings.TrimRight(name[:40], "-")
	}
	return "claude-" + name + "-" + hex.EncodeToString(b)
}

func newClaim(name, pool string, src source, shutdown time.Time) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": claimGVR.GroupVersion().String(),
		"kind":       "SandboxClaim",
		"metadata": map[string]any{
			"name":        name,
			"labels":      map[string]any{managedByLabel: managedBy},
			"annotations": map[string]any{repoAnnotation: src.Repo, refAnnotation: src.Ref},
		},
		"spec": map[string]any{
			"warmPoolRef": map[string]any{"name": pool},
			"lifecycle": map[string]any{
				"shutdownPolicy": "Delete",
				"shutdownTime":   shutdown.UTC().Format(time.RFC3339),
			},
		},
	}}
}

func (k *kube) createClaim(ctx context.Context, src source, shutdown time.Time) (string, error) {
	c, err := k.claims().Create(ctx, newClaim(claimName(src.Repo), k.cfg.Pool, src, shutdown), metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create claim: %w", err)
	}
	return c.GetName(), nil
}

func (k *kube) deleteClaim(ctx context.Context, name string) error {
	policy := metav1.DeletePropagationForeground
	err := k.claims().Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func claimSource(c *unstructured.Unstructured) source {
	a := c.GetAnnotations()
	return source{Repo: a[repoAnnotation], Ref: a[refAnnotation]}
}

// waitForPod follows claim -> sandbox -> pod selector until the pod is ready.
func (k *kube) waitForPod(ctx context.Context, name string) (source, string, error) {
	spin := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	defer fmt.Print("\r\033[K")
	for i := 0; ; i++ {
		c, err := k.claims().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return source{}, "", fmt.Errorf("claim %s: %w", name, err)
		}
		status := "waiting for sandbox"
		if sb, _, _ := unstructured.NestedString(c.Object, "status", "sandbox", "name"); sb != "" {
			pod, err := k.readyPod(ctx, sb)
			if err != nil {
				return source{}, "", err
			}
			if pod != "" {
				return claimSource(c), pod, nil
			}
			status = "waiting for pod " + sb
		}
		fmt.Printf("\r\033[K%s %s", spin[i%len(spin)], status)
		select {
		case <-ctx.Done():
			return source{}, "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (k *kube) readyPod(ctx context.Context, sandbox string) (string, error) {
	sb, err := k.dyn.Resource(sandboxGVR).Namespace(k.cfg.Namespace).Get(ctx, sandbox, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	selector, _, _ := unstructured.NestedString(sb.Object, "status", "selector")
	if selector == "" {
		return "", nil
	}
	pods, err := k.core.CoreV1().Pods(k.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", err
	}
	for _, p := range pods.Items {
		if p.Status.Phase != corev1.PodRunning || p.DeletionTimestamp != nil {
			continue
		}
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return p.Name, nil
			}
		}
	}
	return "", nil
}

func (k *kube) listClaims(ctx context.Context) ([]unstructured.Unstructured, error) {
	l, err := k.claims().List(ctx, metav1.ListOptions{LabelSelector: managedByLabel + "=" + managedBy})
	if err != nil {
		return nil, err
	}
	return l.Items, nil
}
