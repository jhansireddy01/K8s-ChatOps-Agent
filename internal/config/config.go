// Package config centralizes all runtime configuration, sourced from
// environment variables (with sane local-dev defaults). Nothing here
// should reach out to the network - it's pure config assembly so the
// rest of the app can be tested without a live cluster.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	// Kubernetes
	KubeconfigPath string // "" => in-cluster config
	KubeContext    string // "" => current-context in kubeconfig

	// ArgoCD
	ArgoCDServerAddr string // e.g. "argocd.mycompany.com:443" or "localhost:8080"
	ArgoCDAuthToken  string // ArgoCD API token (argocd account generate-token)
	ArgoCDInsecure   bool   // skip TLS verify (self-signed certs, port-forward)
	ArgoCDPlaintext  bool   // use plain HTTP/gRPC instead of TLS

	// LLM (Ollama)
	OllamaBaseURL string // e.g. "http://localhost:11434"
	OllamaModel   string // must be a tool-calling-capable model

	// Safety
	RequireConfirmation bool // gate all write tools behind an explicit y/N
	AllowedNamespaces   []string
}

func Load() (*Config, error) {
	cfg := &Config{
		KubeconfigPath:      os.Getenv("KUBECONFIG"),
		KubeContext:         os.Getenv("KUBE_CONTEXT"),
		ArgoCDServerAddr:    getenvDefault("ARGOCD_SERVER", "localhost:8080"),
		ArgoCDAuthToken:     os.Getenv("ARGOCD_AUTH_TOKEN"),
		ArgoCDInsecure:      os.Getenv("ARGOCD_INSECURE") == "true",
		ArgoCDPlaintext:     os.Getenv("ARGOCD_PLAINTEXT") == "true",
		OllamaBaseURL:       getenvDefault("OLLAMA_BASE_URL", "http://localhost:11434"),
		OllamaModel:         getenvDefault("OLLAMA_MODEL", "qwen2.5:7b"),
		RequireConfirmation: os.Getenv("SKIP_CONFIRMATION") != "true",
	}

	if cfg.ArgoCDAuthToken == "" {
		return nil, fmt.Errorf("ARGOCD_AUTH_TOKEN is required (see: argocd account generate-token)")
	}

	return cfg, nil
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
