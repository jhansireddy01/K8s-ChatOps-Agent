// Command cli is the interactive REPL for the ChatOps agent. Run it,
// type questions in plain English, approve/deny any write actions it
// proposes, and Ctrl+D or "exit" to quit.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yourorg/chatops-agent/internal/agent"
	"github.com/yourorg/chatops-agent/internal/argocdclient"
	"github.com/yourorg/chatops-agent/internal/config"
	"github.com/yourorg/chatops-agent/internal/k8sclient"
	"github.com/yourorg/chatops-agent/internal/llm"
	"github.com/yourorg/chatops-agent/internal/tools"
)

// cliConfirmer implements agent.Confirmer by prompting on stdin.
type cliConfirmer struct {
	in *bufio.Reader
}

func (c *cliConfirmer) Confirm(toolName, argsPreview string) (bool, error) {
	fmt.Printf("\n⚠️  About to run WRITE action %q with args:\n%s\nProceed? [y/N]: ", toolName, argsPreview)
	line, err := c.in.ReadString('\n')
	if err != nil {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	k8sClient, err := k8sclient.New(cfg.KubeconfigPath, cfg.KubeContext)
	if err != nil {
		return fmt.Errorf("building k8s client: %w", err)
	}

	argoClient := argocdclient.New(cfg.ArgoCDServerAddr, cfg.ArgoCDAuthToken, cfg.ArgoCDInsecure, cfg.ArgoCDPlaintext)

	registry := tools.NewRegistry(
		tools.NewListPodsTool(k8sClient),
		tools.NewPodLogsTool(k8sClient),
		tools.NewDescribeDeploymentTool(k8sClient),
		tools.NewScaleDeploymentTool(k8sClient),
		tools.NewListEventsTool(k8sClient),
		tools.NewListApplicationsTool(argoClient),
		tools.NewListOutOfSyncTool(argoClient),
		tools.NewGetApplicationTool(argoClient),
		tools.NewSyncApplicationTool(argoClient),
		tools.NewGetApplicationDiffTool(argoClient),
	)

	llmClient := llm.NewClient(cfg.OllamaBaseURL, cfg.OllamaModel)
	reader := bufio.NewReader(os.Stdin)
	a := agent.New(llmClient, registry, &cliConfirmer{in: reader})

	fmt.Println("ChatOps agent ready. Model:", cfg.OllamaModel)
	fmt.Println(`Try: "which apps are out of sync in the prod project?"`)
	fmt.Println(`Try: "scale down the staging repo-server to 1 replica"`)
	fmt.Println("Type 'exit' or Ctrl+D to quit.")

	ctx := context.Background()
	for {
		fmt.Print("\n> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			return nil // EOF (Ctrl+D)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			return nil
		}

		answer, err := a.Ask(ctx, line)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			continue
		}
		fmt.Println(answer)
	}
}
