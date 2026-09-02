// Package k8sclient builds a k8s.io/client-go Clientset from either a
// kubeconfig file (local dev) or in-cluster config (when running as a pod
// with a ServiceAccount - see deploy/rbac.yaml).
package k8sclient

import (
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func New(kubeconfigPath, context string) (*kubernetes.Clientset, error) {
	restCfg, err := buildRestConfig(kubeconfigPath, context)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restCfg)
}

func buildRestConfig(kubeconfigPath, kubeContext string) (*rest.Config, error) {
	// Running inside the cluster (deployed as a pod) - use the mounted
	// ServiceAccount token instead of a kubeconfig file.
	if kubeconfigPath == "" {
		if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
			cfg, err := rest.InClusterConfig()
			if err == nil {
				return cfg, nil
			}
		}
		if home, err := os.UserHomeDir(); err == nil {
			kubeconfigPath = home + "/.kube/config"
		}
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig %q: %w", kubeconfigPath, err)
	}
	return cfg, nil
}
