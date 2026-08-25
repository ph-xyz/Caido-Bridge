package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoreplay"
	"github.com/ph-xyz/Caido-Bridge/internal/config"
	"github.com/ph-xyz/Caido-Bridge/internal/tools"
)

const version = "0.4.0"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		printUsage(stdout)
		if len(args) == 0 {
			return nil
		}
		return fmt.Errorf("expected exactly one command")
	}

	switch args[0] {
	case "serve":
		return runServe(stderr)
	case "doctor":
		return runDoctor(stdout)
	case "version", "--version", "-version":
		fmt.Fprintf(stdout, "CaidoBridge v%s\n", version)
		return nil
	case "help", "--help", "-h":
		printUsage(stdout)
		return nil
	default:
		printUsage(stdout)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "CaidoBridge v"+version)
	fmt.Fprintln(w, "usage: CaidoBridge <serve|doctor|version>")
}

func newConfiguredClient() (config.Config, *caidoread.Client, error) {
	cfg, err := config.FromEnv()
	if err != nil {
		return config.Config{}, nil, err
	}
	client, err := caidoread.New(cfg.URL, cfg.Token)
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("create Caido client: %w", err)
	}
	return cfg, client, nil
}

func runServe(stderr io.Writer) error {
	cfg, client, err := newConfiguredClient()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Connect(checkCtx); err != nil {
		return fmt.Errorf("Caido is not reachable: %w", err)
	}
	if _, err := client.Runtime(checkCtx); err != nil {
		return fmt.Errorf("Caido authentication/read check failed: %w", err)
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: "CaidoBridge", Version: version},
		&mcp.ServerOptions{
			Instructions: serverInstructions(cfg.ReplayEnabled),
			Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
			Capabilities: &mcp.ServerCapabilities{},
		},
	)
	previews := tools.RegisterAll(server, client)
	if cfg.ReplayEnabled {
		replayClient, err := caidoreplay.New(cfg.URL, cfg.Token)
		if err != nil {
			return fmt.Errorf("create active Replay client: %w", err)
		}
		tools.RegisterActiveReplay(server, client, replayClient, previews)
	}

	fmt.Fprintf(stderr, "CaidoBridge v%s\n", version)
	fmt.Fprintf(stderr, "Caido URL: %s\n", cfg.URL)
	fmt.Fprintln(stderr, "Auth: CAIDO_ACCESS_TOKEN configured (value hidden)")
	fmt.Fprintf(
		stderr,
		"Tools: %d total (%d read-only, %d active)\n",
		tools.ToolCount(cfg.ReplayEnabled),
		tools.ReadOnlyToolCount,
		activeToolCount(cfg.ReplayEnabled),
	)
	if cfg.ReplayEnabled {
		fmt.Fprintln(stderr, "Replay: ENABLED (preview, scope, identity, and confirmation guards active; no cumulative request budget)")
	} else {
		fmt.Fprintln(stderr, "Replay: disabled (set CAIDO_ENABLE_REPLAY=1 to register active tools)")
	}
	fmt.Fprintln(stderr, "Transport: stdio (waiting for MCP client)")

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("MCP server: %w", err)
	}
	return nil
}

func runDoctor(stdout io.Writer) error {
	cfg, client, err := newConfiguredClient()
	if err != nil {
		return fmt.Errorf("configuration check failed: %w", err)
	}
	fmt.Fprintf(stdout, "[ok] CAIDO_URL: %s\n", cfg.URL)
	fmt.Fprintln(stdout, "[ok] CAIDO_ACCESS_TOKEN: configured (value hidden)")
	if cfg.ReplayEnabled {
		fmt.Fprintln(stdout, "[warning] Active Replay capability is ENABLED")
	} else {
		fmt.Fprintln(stdout, "[ok] Active Replay capability is disabled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("local Caido health check failed: %w", err)
	}
	fmt.Fprintln(stdout, "[ok] Local Caido health endpoint is reachable")

	runtime, err := client.Runtime(ctx)
	if err != nil {
		return fmt.Errorf("authentication/read check failed: %w", err)
	}
	fmt.Fprintf(stdout, "[ok] Authenticated read access; Caido %s (%s)\n", runtime.Version, runtime.Platform)

	project, err := client.CurrentProject(ctx)
	if err != nil {
		return fmt.Errorf("current project check failed: %w", err)
	}
	fmt.Fprintf(
		stdout,
		"[ok] MCP current project: %s (%s)\n",
		project.Name,
		project.ID,
	)

	page, err := client.ListRequests(ctx, caidoread.ListRequestsParams{Limit: 1})
	if err != nil {
		return fmt.Errorf("read-only HTTP History query failed: %w", err)
	}
	fmt.Fprintf(stdout, "[ok] Read-only HTTP History query returned %d item(s)\n", len(page.Requests))
	fmt.Fprintln(stdout, "doctor: all checks passed")
	return nil
}

func activeToolCount(enabled bool) int {
	if enabled {
		return tools.ActiveToolCount
	}
	return 0
}

func serverInstructions(replayEnabled bool) string {
	base := "Call caido_get_current_project before every project-scoped operation and pass its ID as projectId. Never continue after an invalid, missing, nonexistent, changed, or mismatched project. Every project-scoped result identifies its source project. Select an exact Caido scopeId for every scope check or Replay operation. Existing observation tools are strictly read-only and never select projects. caido_preview_replay performs no target request."
	if !replayEnabled {
		return base + " Active Replay is disabled; no tool can send target traffic."
	}
	return base + " Active Replay is explicitly enabled. Before any send, call caido_preview_replay, independently confirm method+host+path/fingerprint, pass its one-use previewToken without changing the prepared request, use one primary mutation per hypothesis, require in-scope same-host traffic, set confirmExecution=true, and set state-changing confirmations when applicable. caido_test_hypothesis emits evidence, never a vulnerability verdict. The server imposes no cumulative active-request budget across a hunt, turn, or chat: do not stop solely because an arbitrary request count was reached. Continue sequential evidence-driven tests until the requested objective is complete, the user stops, or another guard blocks execution."
}
