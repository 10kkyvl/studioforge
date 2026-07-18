package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/10kkyvl/studioforge/internal/app"
	"github.com/10kkyvl/studioforge/internal/config"
	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/diagnostics"
	"github.com/10kkyvl/studioforge/internal/platform"
	"github.com/10kkyvl/studioforge/internal/portable"
	"github.com/10kkyvl/studioforge/internal/processes"
	"github.com/10kkyvl/studioforge/internal/providers/claudecode"
	"github.com/10kkyvl/studioforge/internal/providers/codex"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
	"github.com/10kkyvl/studioforge/internal/rojo"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("StudioForge stopped", "error", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "doctor":
			return doctorCommand(args[1:])
		case "export":
			return exportCommand(args[1:])
		case "import":
			return importCommand(args[1:])
		case "mcp-shim":
			return mcpShimCommand(args[1:])
		}
	}
	fs := flag.NewFlagSet("studioforge", flag.ContinueOnError)
	var opts config.Options
	var version bool
	fs.StringVar(&opts.Host, "host", "127.0.0.1", "listener host (loopback only without --unsafe-host)")
	fs.IntVar(&opts.Port, "port", 0, "listener port; 0 chooses a free port")
	fs.StringVar(&opts.DataDir, "data-dir", "", "application data directory")
	fs.BoolVar(&opts.NoOpen, "no-open", false, "do not open a browser")
	fs.StringVar(&opts.LogLevel, "log-level", "info", "debug, info, warn, or error")
	fs.BoolVar(&opts.SafeMode, "safe-mode", false, "disable AI, MCP, and Rojo workers")
	fs.BoolVar(&opts.MockMode, "mock", false, "seed and run the deterministic demo")
	fs.BoolVar(&opts.UnsafeHost, "unsafe-host", false, "allow a non-loopback listener (unsafe)")
	fs.BoolVar(&version, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if version {
		fmt.Printf("StudioForge %s (%s, %s)\n", config.Version, config.Commit, config.BuildDate)
		return nil
	}
	if err := configureLogging(opts.LogLevel); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return app.Run(ctx, opts)
}
func configureLogging(level string) error {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "", "info":
		l = slog.LevelInfo
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		return fmt.Errorf("unsupported log level %q", level)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
	return nil
}

func openData(ctx context.Context, override string) (string, *database.DB, *database.Store, error) {
	dataDir, err := platform.DataDir(override)
	if err != nil {
		return "", nil, nil, err
	}
	if err := platform.EnsurePrivateDirs(dataDir); err != nil {
		return "", nil, nil, err
	}
	db, err := database.Open(ctx, filepath.Join(dataDir, "studioforge.db"))
	if err != nil {
		return "", nil, nil, err
	}
	return dataDir, db, database.NewStore(db), nil
}
func doctorCommand(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dataDirFlag := fs.String("data-dir", "", "application data directory")
	bundle := fs.String("bundle", "", "write redacted diagnostic bundle zip")
	mockMode := fs.Bool("mock", false, "report mock mode")
	safeMode := fs.Bool("safe-mode", false, "report safe mode")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dataDir, db, store, err := openData(ctx, *dataDirFlag)
	if err != nil {
		return err
	}
	defer db.Close()
	supervisor := processes.NewSupervisor()
	defer supervisor.Close(context.Background())
	setting := func(key string) string {
		value, _, _ := store.Setting(ctx, key)
		return value
	}
	doc := &diagnostics.Doctor{DB: db, DataDir: dataDir, MockMode: *mockMode, SafeMode: *safeMode, Claude: claudecode.New(setting("claude_path")), Codex: codex.New(setting("codex_path")), Rojo: rojo.New(supervisor, setting("rojo_path")), GitOverride: setting("git_path"), MCPOverride: setting("studio_mcp_path")}
	report := doc.Run(ctx)
	body, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(body))
	if *bundle != "" {
		if err := doc.ExportBundle(ctx, *bundle); err != nil {
			return err
		}
		fmt.Println("diagnostic bundle:", *bundle)
	}
	if report.Database != "ok" {
		return errors.New("doctor found a database error")
	}
	return nil
}
func exportCommand(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	dataDirFlag := fs.String("data-dir", "", "application data directory")
	projectID := fs.String("project", "", "project ID")
	output := fs.String("output", "", "output .zip path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *projectID == "" || *output == "" {
		return errors.New("export requires --project and --output")
	}
	ctx := context.Background()
	_, db, store, err := openData(ctx, *dataDirFlag)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := portable.Export(ctx, store, *projectID, *output); err != nil {
		return err
	}
	fmt.Println("export created:", *output)
	return nil
}
func importCommand(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	dataDirFlag := fs.String("data-dir", "", "application data directory")
	input := fs.String("file", "", "portable .zip path")
	apply := fs.Bool("apply", false, "apply after preview")
	root := fs.String("path", "", "existing project root override")
	name := fs.String("name", "", "project name override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("import requires --file")
	}
	ctx := context.Background()
	_, db, store, err := openData(ctx, *dataDirFlag)
	if err != nil {
		return err
	}
	defer db.Close()
	preview, err := portable.Inspect(ctx, store, *input)
	if err != nil {
		return err
	}
	body, _ := json.MarshalIndent(preview, "", "  ")
	fmt.Println(string(body))
	if *apply {
		project, err := portable.Apply(ctx, store, *input, *root, *name)
		if err != nil {
			return err
		}
		fmt.Println("imported project:", project.ID)
	}
	return nil
}

// mcpShimCommand serves Studio's MCP tools to one agent. StudioForge points a
// run's generated config here rather than at the launcher, because the launcher
// advertises tools only to whichever process won the WS host port and would
// otherwise tell the agent Studio has no tools at all.
//
// It speaks the MCP stdio protocol, so its stdout carries protocol traffic and
// nothing else; diagnostics belong on stderr.
func mcpShimCommand(args []string) error {
	fs := flag.NewFlagSet("mcp-shim", flag.ContinueOnError)
	launcher := fs.String("launcher", "", "Studio MCP launcher command")
	cache := fs.String("tool-cache", "", "path remembering the tool list Studio last published")
	var launcherArgs stringList
	fs.Var(&launcherArgs, "launcher-arg", "argument for the launcher command (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *launcher == "" {
		return errors.New("mcp-shim requires --launcher")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return mcp.Serve(ctx, os.Stdin, os.Stdout, mcp.ShimOptions{
		Launch:    mcp.LaunchConfig{Command: *launcher, Args: launcherArgs},
		CachePath: *cache,
	})
}

// stringList collects a repeatable flag, so launcher arguments survive with
// their boundaries intact instead of being re-split out of one string.
type stringList []string

func (l *stringList) String() string { return fmt.Sprint(*l) }
func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}
