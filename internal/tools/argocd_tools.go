package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yourorg/chatops-agent/internal/argocdclient"
)

// ---------- argocd_list_applications ----------

type listAppsArgs struct {
	Project string `json:"project"`
}

func NewListApplicationsTool(c *argocdclient.Client) Tool { return &listAppsTool{client: c} }

type listAppsTool struct{ client *argocdclient.Client }

func (t *listAppsTool) Name() string { return "argocd_list_applications" }
func (t *listAppsTool) Description() string {
	return "List ArgoCD applications, optionally filtered by project, with sync and health status. Read-only."
}
func (t *listAppsTool) IsWrite() bool { return false }
func (t *listAppsTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"project": {Type: "string", Description: "ArgoCD project name to filter by (optional, empty = all projects)"},
	}, nil)
}
func (t *listAppsTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[listAppsArgs](raw)
	if err != nil {
		return "", err
	}
	apps, err := t.client.ListApplications(args.Project)
	if err != nil {
		return "", err
	}
	return formatApps(apps), nil
}

// ---------- argocd_list_out_of_sync ----------

func NewListOutOfSyncTool(c *argocdclient.Client) Tool { return &listOutOfSyncTool{client: c} }

type listOutOfSyncTool struct{ client *argocdclient.Client }

func (t *listOutOfSyncTool) Name() string { return "argocd_list_out_of_sync" }
func (t *listOutOfSyncTool) Description() string {
	return "List ArgoCD applications whose live state has drifted from Git (Sync status != Synced), optionally filtered by project. Read-only."
}
func (t *listOutOfSyncTool) IsWrite() bool { return false }
func (t *listOutOfSyncTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"project": {Type: "string", Description: "ArgoCD project name to filter by (optional, empty = all projects)"},
	}, nil)
}
func (t *listOutOfSyncTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[listAppsArgs](raw)
	if err != nil {
		return "", err
	}
	apps, err := t.client.ListApplications(args.Project)
	if err != nil {
		return "", err
	}
	var oos []argocdclient.Application
	for _, a := range apps {
		if a.Status.Sync.Status != "Synced" {
			oos = append(oos, a)
		}
	}
	if len(oos) == 0 {
		return "All applications are in sync.", nil
	}
	return formatApps(oos), nil
}

// ---------- argocd_get_application ----------

type getAppArgs struct {
	Name string `json:"name"`
}

func NewGetApplicationTool(c *argocdclient.Client) Tool { return &getAppTool{client: c} }

type getAppTool struct{ client *argocdclient.Client }

func (t *getAppTool) Name() string { return "argocd_get_application" }
func (t *getAppTool) Description() string {
	return "Get full sync/health/operation status for a single ArgoCD application by name. Read-only."
}
func (t *getAppTool) IsWrite() bool { return false }
func (t *getAppTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"name": {Type: "string", Description: "ArgoCD application name"},
	}, []string{"name"})
}
func (t *getAppTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[getAppArgs](raw)
	if err != nil {
		return "", err
	}
	app, err := t.client.GetApplication(args.Name)
	if err != nil {
		return "", err
	}
	return formatApps([]argocdclient.Application{*app}), nil
}

// ---------- argocd_sync_application (WRITE) ----------

type syncAppArgs struct {
	Name  string `json:"name"`
	Prune bool   `json:"prune"`
}

func NewSyncApplicationTool(c *argocdclient.Client) Tool { return &syncAppTool{client: c} }

type syncAppTool struct{ client *argocdclient.Client }

func (t *syncAppTool) Name() string { return "argocd_sync_application" }
func (t *syncAppTool) Description() string {
	return "Trigger a sync of an ArgoCD application, applying the desired Git state to the cluster. WRITE action - requires user confirmation."
}
func (t *syncAppTool) IsWrite() bool { return true }
func (t *syncAppTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"name":  {Type: "string", Description: "ArgoCD application name"},
		"prune": {Type: "boolean", Description: "Whether to prune resources no longer defined in Git (default false)"},
	}, []string{"name"})
}
func (t *syncAppTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[syncAppArgs](raw)
	if err != nil {
		return "", err
	}
	if err := t.client.SyncApplication(args.Name, args.Prune); err != nil {
		return "", err
	}
	return fmt.Sprintf("Sync triggered for application %q (prune=%v).", args.Name, args.Prune), nil
}

// ---------- argocd_get_application_diff ----------

func NewGetApplicationDiffTool(c *argocdclient.Client) Tool { return &getAppDiffTool{client: c} }

type getAppDiffTool struct{ client *argocdclient.Client }

func (t *getAppDiffTool) Name() string { return "argocd_get_application_diff" }
func (t *getAppDiffTool) Description() string {
	return "Get the live-vs-desired-state diff for an application's managed resources. Read-only."
}
func (t *getAppDiffTool) IsWrite() bool { return false }
func (t *getAppDiffTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"name": {Type: "string", Description: "ArgoCD application name"},
	}, []string{"name"})
}
func (t *getAppDiffTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[getAppArgs](raw)
	if err != nil {
		return "", err
	}
	diff, err := t.client.ManagedResourcesDiff(args.Name)
	if err != nil {
		return "", err
	}
	return string(diff), nil
}

// ---------- shared formatting ----------

func formatApps(apps []argocdclient.Application) string {
	if len(apps) == 0 {
		return "No applications found."
	}
	var sb strings.Builder
	for _, a := range apps {
		fmt.Fprintf(&sb, "%s\tproject=%s\tns=%s\tsync=%s\thealth=%s",
			a.Metadata.Name, a.Spec.Project, a.Spec.Destination.Namespace, a.Status.Sync.Status, a.Status.Health.Status)
		if a.Status.OperationState.Phase != "" {
			fmt.Fprintf(&sb, "\top_phase=%s", a.Status.OperationState.Phase)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
