// Package argocdclient talks to the ArgoCD API server over its
// grpc-gateway REST interface (same one the `argocd` CLI and Web UI use).
// We deliberately avoid the official argocd Go SDK - it pulls in a huge
// dependency tree (the whole argocd-server binary's deps). Plain REST +
// a bearer token is all we need for the handful of read/write calls
// this agent makes.
package argocdclient

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(serverAddr, token string, insecureSkipVerify, plaintext bool) *Client {
	scheme := "https"
	if plaintext {
		scheme = "http"
	}
	transport := &http.Transport{}
	if insecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit opt-in via ARGOCD_INSECURE
	}
	return &Client{
		baseURL: fmt.Sprintf("%s://%s", scheme, serverAddr),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}
}

func (c *Client) do(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling argocd at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("argocd API returned %d for %s %s: %s", resp.StatusCode, method, path, string(raw))
	}

	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decoding argocd response for %s: %w (body: %s)", path, err, string(raw))
		}
	}
	return nil
}

// --- Types (trimmed down to the fields we actually use) ---

type Application struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Project     string `json:"project"`
		Destination struct {
			Namespace string `json:"namespace"`
			Server    string `json:"server"`
		} `json:"destination"`
	} `json:"spec"`
	Status struct {
		Sync struct {
			Status string `json:"status"` // Synced | OutOfSync | Unknown
		} `json:"sync"`
		Health struct {
			Status string `json:"status"` // Healthy | Degraded | Progressing | ...
		} `json:"health"`
		OperationState struct {
			Phase   string `json:"phase"`
			Message string `json:"message"`
		} `json:"operationState"`
	} `json:"status"`
}

type ApplicationList struct {
	Items []Application `json:"items"`
}

// --- Calls ---

// ListApplications returns all applications, optionally filtered by
// project (empty string = all projects).
func (c *Client) ListApplications(project string) ([]Application, error) {
	path := "/api/v1/applications"
	if project != "" {
		path += "?projects=" + project
	}
	var out ApplicationList
	if err := c.do(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// GetApplication fetches a single application by name.
func (c *Client) GetApplication(name string) (*Application, error) {
	var out Application
	if err := c.do(http.MethodGet, "/api/v1/applications/"+name, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type syncRequest struct {
	Prune  bool `json:"prune"`
	DryRun bool `json:"dryRun"`
}

// SyncApplication triggers a sync. This is a WRITE action - callers
// (internal/agent) must gate it behind explicit user confirmation.
func (c *Client) SyncApplication(name string, prune bool) error {
	return c.do(http.MethodPost, "/api/v1/applications/"+name+"/sync", syncRequest{Prune: prune}, nil)
}

// ManagedResourcesDiff returns raw JSON describing live-vs-desired diffs
// for the application's managed resources (used by the diff tool).
func (c *Client) ManagedResourcesDiff(name string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(http.MethodGet, "/api/v1/applications/"+name+"/managed-resources", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
