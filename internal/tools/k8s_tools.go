package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func mustUnmarshal[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

// ---------- k8s_list_pods ----------

type listPodsArgs struct {
	Namespace     string `json:"namespace"`
	LabelSelector string `json:"label_selector"`
}

func NewListPodsTool(c kubernetes.Interface) Tool { return &listPodsTool{client: c} }

type listPodsTool struct{ client kubernetes.Interface }

func (t *listPodsTool) Name() string { return "k8s_list_pods" }
func (t *listPodsTool) Description() string {
	return "List pods in a namespace, optionally filtered by a label selector (e.g. 'app=repo-server'). Read-only."
}
func (t *listPodsTool) IsWrite() bool { return false }
func (t *listPodsTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"namespace":      {Type: "string", Description: "Kubernetes namespace to list pods in"},
		"label_selector": {Type: "string", Description: "Optional label selector, e.g. app=repo-server"},
	}, []string{"namespace"})
}
func (t *listPodsTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[listPodsArgs](raw)
	if err != nil {
		return "", err
	}
	pods, err := t.client.CoreV1().Pods(args.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: args.LabelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("listing pods in %s: %w", args.Namespace, err)
	}
	var sb strings.Builder
	if len(pods.Items) == 0 {
		return "No pods found.", nil
	}
	for _, p := range pods.Items {
		ready, total := countReady(p)
		fmt.Fprintf(&sb, "%s\tstatus=%s\tready=%d/%d\trestarts=%d\tnode=%s\n",
			p.Name, p.Status.Phase, ready, total, totalRestarts(p), p.Spec.NodeName)
	}
	return sb.String(), nil
}

func countReady(p corev1.Pod) (ready, total int) {
	for _, cs := range p.Status.ContainerStatuses {
		total++
		if cs.Ready {
			ready++
		}
	}
	return
}

func totalRestarts(p corev1.Pod) int32 {
	var n int32
	for _, cs := range p.Status.ContainerStatuses {
		n += cs.RestartCount
	}
	return n
}

// ---------- k8s_get_pod_logs ----------

type podLogsArgs struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
	TailLines int64  `json:"tail_lines"`
}

func NewPodLogsTool(c kubernetes.Interface) Tool { return &podLogsTool{client: c} }

type podLogsTool struct{ client kubernetes.Interface }

func (t *podLogsTool) Name() string { return "k8s_get_pod_logs" }
func (t *podLogsTool) Description() string {
	return "Fetch recent log lines from a pod (optionally a specific container). Read-only."
}
func (t *podLogsTool) IsWrite() bool { return false }
func (t *podLogsTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"namespace":  {Type: "string", Description: "Namespace the pod lives in"},
		"pod":        {Type: "string", Description: "Pod name"},
		"container":  {Type: "string", Description: "Container name (optional; defaults to the only/first container)"},
		"tail_lines": {Type: "integer", Description: "Number of lines to tail from the end (default 100)"},
	}, []string{"namespace", "pod"})
}
func (t *podLogsTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[podLogsArgs](raw)
	if err != nil {
		return "", err
	}
	tail := args.TailLines
	if tail <= 0 {
		tail = 100
	}
	req := t.client.CoreV1().Pods(args.Namespace).GetLogs(args.Pod, &corev1.PodLogOptions{
		Container: args.Container,
		TailLines: &tail,
	})
	stream, err := req.Stream(context.Background())
	if err != nil {
		return "", fmt.Errorf("fetching logs for %s/%s: %w", args.Namespace, args.Pod, err)
	}
	defer stream.Close()

	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	if sb.Len() == 0 {
		return "(no log output)", nil
	}
	return sb.String(), nil
}

// ---------- k8s_describe_deployment ----------

type describeDeployArgs struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func NewDescribeDeploymentTool(c kubernetes.Interface) Tool {
	return &describeDeploymentTool{client: c}
}

type describeDeploymentTool struct{ client kubernetes.Interface }

func (t *describeDeploymentTool) Name() string { return "k8s_describe_deployment" }
func (t *describeDeploymentTool) Description() string {
	return "Get status details of a Deployment: replicas, available/updated counts, conditions, image. Read-only."
}
func (t *describeDeploymentTool) IsWrite() bool { return false }
func (t *describeDeploymentTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"namespace": {Type: "string", Description: "Namespace the deployment lives in"},
		"name":      {Type: "string", Description: "Deployment name"},
	}, []string{"namespace", "name"})
}
func (t *describeDeploymentTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[describeDeployArgs](raw)
	if err != nil {
		return "", err
	}
	d, err := t.client.AppsV1().Deployments(args.Namespace).Get(context.Background(), args.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting deployment %s/%s: %w", args.Namespace, args.Name, err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "deployment=%s namespace=%s\n", d.Name, d.Namespace)
	fmt.Fprintf(&sb, "desired=%d ready=%d available=%d updated=%d\n",
		derefInt32(d.Spec.Replicas), d.Status.ReadyReplicas, d.Status.AvailableReplicas, d.Status.UpdatedReplicas)
	if len(d.Spec.Template.Spec.Containers) > 0 {
		fmt.Fprintf(&sb, "image=%s\n", d.Spec.Template.Spec.Containers[0].Image)
	}
	for _, c := range d.Status.Conditions {
		fmt.Fprintf(&sb, "condition: %s=%s (%s) %s\n", c.Type, c.Status, c.Reason, c.Message)
	}
	return sb.String(), nil
}

func derefInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

// ---------- k8s_scale_deployment (WRITE) ----------

type scaleDeployArgs struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

func NewScaleDeploymentTool(c kubernetes.Interface) Tool { return &scaleDeploymentTool{client: c} }

type scaleDeploymentTool struct{ client kubernetes.Interface }

func (t *scaleDeploymentTool) Name() string { return "k8s_scale_deployment" }
func (t *scaleDeploymentTool) Description() string {
	return "Scale a Deployment to a target replica count. WRITE action - requires user confirmation."
}
func (t *scaleDeploymentTool) IsWrite() bool { return true }
func (t *scaleDeploymentTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"namespace": {Type: "string", Description: "Namespace the deployment lives in"},
		"name":      {Type: "string", Description: "Deployment name"},
		"replicas":  {Type: "integer", Description: "Target replica count"},
	}, []string{"namespace", "name", "replicas"})
}
func (t *scaleDeploymentTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[scaleDeployArgs](raw)
	if err != nil {
		return "", err
	}
	scale, err := t.client.AppsV1().Deployments(args.Namespace).GetScale(context.Background(), args.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting scale for %s/%s: %w", args.Namespace, args.Name, err)
	}
	prev := scale.Spec.Replicas
	scale.Spec.Replicas = args.Replicas
	if _, err := t.client.AppsV1().Deployments(args.Namespace).UpdateScale(context.Background(), args.Name, scale, metav1.UpdateOptions{}); err != nil {
		return "", fmt.Errorf("scaling %s/%s: %w", args.Namespace, args.Name, err)
	}
	return fmt.Sprintf("Scaled %s/%s from %d to %d replicas.", args.Namespace, args.Name, prev, args.Replicas), nil
}

// ---------- k8s_list_events ----------

type listEventsArgs struct {
	Namespace string `json:"namespace"`
}

func NewListEventsTool(c kubernetes.Interface) Tool { return &listEventsTool{client: c} }

type listEventsTool struct{ client kubernetes.Interface }

func (t *listEventsTool) Name() string { return "k8s_list_events" }
func (t *listEventsTool) Description() string {
	return "List recent Kubernetes events (warnings/errors/etc) in a namespace. Read-only."
}
func (t *listEventsTool) IsWrite() bool { return false }
func (t *listEventsTool) Params() paramsSpecType {
	return newParams(map[string]propSpecType{
		"namespace": {Type: "string", Description: "Namespace to list events in"},
	}, []string{"namespace"})
}
func (t *listEventsTool) Execute(raw json.RawMessage) (string, error) {
	args, err := mustUnmarshal[listEventsArgs](raw)
	if err != nil {
		return "", err
	}
	events, err := t.client.CoreV1().Events(args.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing events in %s: %w", args.Namespace, err)
	}
	if len(events.Items) == 0 {
		return "No events found.", nil
	}
	var sb strings.Builder
	for _, e := range events.Items {
		fmt.Fprintf(&sb, "[%s] %s/%s: %s (count=%d)\n", e.Type, e.InvolvedObject.Kind, e.InvolvedObject.Name, e.Message, e.Count)
	}
	return sb.String(), nil
}
