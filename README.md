# chatops-agent

A conversational agent for Kubernetes + ArgoCD. Ask things in plain English —
"which apps are out of sync in the prod project?", "scale down the staging
repo-server to 1 replica" — and it translates that into real tool calls
against your cluster and ArgoCD API. Read-only tools run automatically;
write actions (scaling, syncing) always stop for an explicit y/N before
touching anything.

Phase 1 (this drop): interactive CLI, local Ollama model.
Phase 2 (later): Slack bot on the same agent core, with Approve/Deny buttons
instead of a terminal prompt.

## How it fits together

```
cmd/cli/main.go          interactive REPL (stdin/stdout), the only piece
                          that knows about the terminal
internal/agent           tool-calling loop: LLM <-> tools, confirmation gate
internal/llm             Ollama client (OpenAI-style tool-calling protocol)
internal/tools           the actual K8s/ArgoCD actions, each tagged read/write
internal/k8sclient       client-go wiring (kubeconfig or in-cluster)
internal/argocdclient    plain REST client for the ArgoCD API server
internal/config          env-var based configuration
deploy/rbac.yaml         RBAC for running this in-cluster later (Slack bot)
```

The `agent` package has zero knowledge of the CLI or Slack — it just needs
something implementing `agent.Confirmer` to approve/deny write actions. That's
the seam where the Slack bot plugs in later (Approve/Deny Slack buttons
implementing the same interface, instead of a stdin prompt).

## Tools currently implemented

Read-only:
- `k8s_list_pods`, `k8s_get_pod_logs`, `k8s_describe_deployment`, `k8s_list_events`
- `argocd_list_applications`, `argocd_list_out_of_sync`, `argocd_get_application`, `argocd_get_application_diff`

Write (gated by confirmation):
- `k8s_scale_deployment`
- `argocd_sync_application`

Add new tools by implementing the `tools.Tool` interface and registering
them in `cmd/cli/main.go` — nothing else needs to change.

## Prerequisites

1. **Go 1.22+**
2. **Ollama**, running locally, with a **tool-calling-capable model** pulled.
   Plain chat models silently ignore the tool definitions and the agent will
   never actually call anything — it'll just hallucinate answers. Known-good
   models: `qwen2.5:7b` (recommended, good balance), `llama3.1:8b`,
   `mistral-nemo`, `firefunction-v2`. Verify with:
   ```bash
   ollama pull qwen2.5:7b
   ollama show qwen2.5:7b | grep -i tools   # should list "tools" as a capability
   ollama serve                              # if not already running as a service
   ```
3. **kubectl access** to your cluster, i.e. a working `~/.kube/config`
   pointing at the context you want the agent to operate on.
4. **An ArgoCD API token**:
   ```bash
   argocd login <your-argocd-server>
   argocd account generate-token --account <your-account>
   ```
   Use a dedicated ArgoCD account/project role scoped to what you actually
   want the bot to see/sync, not your personal admin account.

## Setup

```bash
git clone <this repo>
cd chatops-agent
cp .env.example .env      # fill in ARGOCD_SERVER / ARGOCD_AUTH_TOKEN at minimum
go mod tidy                # fetches k8s.io/client-go etc - needs real internet
export $(grep -v '^#' .env | xargs)   # or use direnv/dotenv of your choice
go run ./cmd/cli
```

> **Note on this delivery:** the sandbox this was built in only allows
> egress to a short domain allowlist (npm/pypi/crates registries, github.com)
> and could not reach `proxy.golang.org` or `k8s.io` to fetch the Kubernetes
> Go modules. The code is syntax-checked (`gofmt` parses every file clean)
> but **not yet compiled**. Run `go mod tidy && go build ./...` on your own
> machine first thing — it'll pull ~3 dependencies (`k8s.io/api`,
> `k8s.io/apimachinery`, `k8s.io/client-go`) and their transitive deps, then
> tell you immediately if anything needs adjusting.

## Example session

```
ChatOps agent ready. Model: qwen2.5:7b
Try: "which apps are out of sync in the prod project?"
Try: "scale down the staging repo-server to 1 replica"
Type 'exit' or Ctrl+D to quit.

> which apps are out of sync in the prod project?
Two apps are out of sync in `prod`: `billing-api` (OutOfSync, Healthy) and
`notifications-worker` (OutOfSync, Degraded). Want me to pull the diff or
sync either of them?

> sync notifications-worker
⚠️  About to run WRITE action "argocd_sync_application" with args:
{
  "name": "notifications-worker"
}
Proceed? [y/N]: y
Sync triggered for notifications-worker. It's now in the Progressing phase -
ask me again in a minute to check health.
```

## Safety model

- **Every write tool is gated in `internal/agent/agent.go`**, not left to the
  model's judgment — `Tool.IsWrite()` is the single source of truth, checked
  right before execution regardless of what the LLM "intended."
- The system prompt also tells the model not to call write tools unless the
  user clearly asked for the action, but that's a second layer, not the
  safety boundary — the confirmation gate is.
- RBAC in `deploy/rbac.yaml` further limits *what* the agent's own credentials
  can touch, independent of the confirmation logic: read access is broad,
  write access (scale) is deliberately scoped to specific namespaces via
  per-namespace `RoleBinding`s rather than a blanket `ClusterRoleBinding`.
- `argocd_sync_application` and `k8s_scale_deployment` are the only write
  tools today. If you add more (e.g. delete-pod, rollback), tag them
  `IsWrite() -> true` and they get the confirmation gate for free.

## Deploying

### Right now: nowhere — it's a local CLI

For phase 1 there's nothing to deploy. You run `go run ./cmd/cli` (or the
built binary) on your laptop, pointed at your kubeconfig and an ArgoCD
token. That's the whole "deployment."

### Docker image (optional, useful for a jump-box or CI)

```bash
docker build -t chatops-agent:latest .
docker run --rm -it \
  -e ARGOCD_SERVER=argocd.mycompany.com:443 \
  -e ARGOCD_AUTH_TOKEN=$ARGOCD_AUTH_TOKEN \
  -e OLLAMA_BASE_URL=http://host.docker.internal:11434 \
  -v $HOME/.kube/config:/root/.kube/config:ro \
  chatops-agent:latest
```
Note Ollama itself isn't containerized here — point `OLLAMA_BASE_URL` at
wherever it's actually running (host machine, another container, etc).

### Phase 2: in-cluster as the Slack bot

When you build the Slack entrypoint (`cmd/slackbot/main.go`, reusing
`internal/agent` + all existing tools), the shape becomes:

1. Build & push the image (same `Dockerfile`, different `ENTRYPOINT`/binary).
2. `kubectl create namespace chatops`
3. `kubectl apply -f deploy/rbac.yaml` — creates the ServiceAccount + roles.
   Note it binds the writer role in the `staging` namespace only; add a
   `RoleBinding` per namespace you want scaling enabled in.
4. Store the ArgoCD token and Slack bot/app tokens as a `Secret`, mount as
   env vars.
5. Deploy with `serviceAccountName: chatops-agent` so it picks up in-cluster
   config automatically (`internal/k8sclient` already detects and prefers
   the mounted ServiceAccount token over a kubeconfig file).
6. Point `OLLAMA_BASE_URL` at wherever Ollama runs in that environment —
   either a sidecar/Deployment inside the cluster or a reachable service
   outside it. For anything beyond a demo, plan for real GPU/CPU capacity;
   this is the one component with real resource requirements.

I haven't written the Slack entrypoint or its Kubernetes Deployment/Secret
manifests yet since you said Slack is a later phase — say the word and I'll
build those next, reusing everything in `internal/agent` and `internal/tools`
unchanged.

## Extending

- **New tool:** implement `tools.Tool` (see any file in `internal/tools/`
  for the pattern), register it in `cmd/cli/main.go`'s `tools.NewRegistry(...)`
  call. Mark `IsWrite() true` for anything that mutates cluster/ArgoCD state.
- **Different LLM backend:** `internal/llm.Client` is the only thing that
  knows about Ollama's wire format. Swap it for an OpenAI/Anthropic client
  implementing the same `Chat(ctx, messages, tools) (*Message, error)` shape
  and nothing in `internal/agent` changes.
- **Multi-cluster:** currently one kubeconfig/context per run. For multiple
  clusters, parameterize `k8sclient.New` calls per-cluster and expose a
  `cluster` argument on the k8s tools.
