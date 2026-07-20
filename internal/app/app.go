package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/10kkyvl/studioforge/internal/api"
	"github.com/10kkyvl/studioforge/internal/config"
	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/diagnostics"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/gitops"
	"github.com/10kkyvl/studioforge/internal/memory"
	"github.com/10kkyvl/studioforge/internal/platform"
	"github.com/10kkyvl/studioforge/internal/processes"
	"github.com/10kkyvl/studioforge/internal/projects"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/claudecode"
	"github.com/10kkyvl/studioforge/internal/providers/codex"
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
	"github.com/10kkyvl/studioforge/internal/roblox/studio"
	"github.com/10kkyvl/studioforge/internal/rojo"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

type Runtime struct {
	Options        config.Options
	DataDir, URL   string
	BootstrapToken string
}

func Run(ctx context.Context, opts config.Options) error {
	if err := opts.Normalize(); err != nil {
		return err
	}
	dataDir, err := platform.DataDir(opts.DataDir)
	if err != nil {
		return err
	}
	if err := platform.EnsurePrivateDirs(dataDir); err != nil {
		return err
	}
	lock, err := platform.AcquireLock(dataDir)
	if err != nil {
		return err
	}
	defer lock.Release()
	db, err := database.Open(ctx, filepath.Join(dataDir, "studioforge.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	store := database.NewStore(db)
	memoryStore := memory.New(db)
	setting := func(key, fallback string) string {
		value, ok, _ := store.Setting(ctx, key)
		if !ok || value == "" {
			return fallback
		}
		return value
	}
	if _, err := store.RecoverInterrupted(ctx); err != nil {
		return fmt.Errorf("recover interrupted runs: %w", err)
	}
	if opts.MockMode {
		if err := store.SeedDemo(ctx, dataDir); err != nil {
			return fmt.Errorf("seed demo: %w", err)
		}
	}
	_ = automaticBackup(ctx, db, store, dataDir)
	guard := projects.NewPathGuard()
	projectList, err := store.ListProjects(ctx, true)
	if err != nil {
		return err
	}
	defaultProvider := setting("default_provider", "codex")
	if opts.MockMode {
		if _, configured, _ := store.Setting(ctx, "default_provider"); !configured {
			defaultProvider = "mock"
		}
	}
	defaultModel := setting("default_model", "default")
	defaultEffort := setting("default_effort", "medium")
	for _, project := range projectList {
		if _, err := guard.Register(project.ID, project.Path); err != nil {
			slog.Warn("project root unavailable", "project", project.ID, "error", err)
		}
		if _, created, err := store.EnsureDefaultAgent(ctx, project.ID, defaultProvider, defaultModel, defaultEffort); err != nil {
			return fmt.Errorf("ensure default agent for %s: %w", project.ID, err)
		} else if created {
			slog.Info("created missing default agent", "project", project.ID, "provider", defaultProvider)
		}
	}
	hub := events.NewHub(store)
	defer hub.Close()
	leases := resources.NewManager(30 * time.Second)
	defer leases.Close()
	supervisor := processes.NewSupervisor()
	mockProvider := mock.New()
	claudeProvider := claudecode.New(setting("claude_path", ""))
	codexProvider := codex.New(setting("codex_path", ""))
	adapters := map[string]providers.Provider{"mock": mockProvider, "claude": claudeProvider, "codex": codexProvider}
	schedulerManager := scheduler.New(ctx, store, hub, leases, adapters)
	schedulerManager.SetMemory(memoryStore)
	if count, err := strconv.Atoi(setting("concurrency", "6")); err == nil {
		schedulerManager.SetLimits(count, 0, 0, 0)
	}
	if opts.SafeMode {
		schedulerManager.SetLimits(1, 1, 1, 1)
	}
	rojoManager := rojo.New(supervisor, setting("rojo_path", ""))
	doctor := &diagnostics.Doctor{DB: db, DataDir: dataDir, SafeMode: opts.SafeMode, MockMode: opts.MockMode, Claude: claudeProvider, Codex: codexProvider, Rojo: rojoManager, MCPOverride: setting("studio_mcp_path", ""), GitOverride: setting("git_path", "")}
	// rojoManager.Start puts its process on the same supervisor every other
	// child process runs under, so the shutdown sequence below
	// (supervisor.Close) already stops a live sync session and frees its port
	// without anything here having to track sessions separately.
	syncer := &syncAdapter{manager: rojoManager}
	differ := &diffAdapter{client: gitops.New()}

	// Grant Claude runs access to Roblox Studio. Only Claude: the Codex adapter
	// has no --mcp-config equivalent, so a grant there would spawn the launcher
	// to no effect.
	var studioMCPOverride atomic.Value
	studioMCPOverride.Store(setting("studio_mcp_path", ""))
	var studioAutoOpen atomic.Value
	studioAutoOpen.Store(setting("studio_auto_open", "true") != "false")
	// playtestWindowSeconds bounds how long the post-run validation loop polls
	// the console in Play mode before classifying the result.
	var playtestWindowSeconds atomic.Int64
	playtestWindowSeconds.Store(30)
	if seconds, err := strconv.Atoi(setting("playtest_window_seconds", "30")); err == nil && seconds > 0 {
		playtestWindowSeconds.Store(int64(seconds))
	}
	studioOpener := &studio.Opener{Rojo: rojoManager}
	studioProvisioner := &mcp.Provisioner{
		Dir: filepath.Join(dataDir, "mcp"),
		Override: func() string {
			value, _ := studioMCPOverride.Load().(string)
			return value
		},
		AutoOpen: func() bool {
			enabled, _ := studioAutoOpen.Load().(bool)
			return enabled
		},
		Running: studio.IsRunning,
	}
	// studioTarget tells the provisioner which open Studio belongs to this run's
	// project, and how to open it when none does. Every project used to build to
	// the same place file name, so an agent could be handed a Studio holding a
	// different project entirely.
	studioTarget := func(ctx context.Context, projectID string) mcp.Target {
		if projectID == "" {
			return mcp.Target{}
		}
		project, err := store.Project(ctx, projectID)
		if err != nil {
			return mcp.Target{}
		}
		return mcp.Target{
			PlaceName: studio.PlaceName(project.Name, project.ID),
			Open: func(ctx context.Context) error {
				_, err := studioOpener.OpenProject(ctx, project.Path, project.Name, project.ID)
				return err
			},
		}
	}
	// The badge polls this, and every uncached call spawns a launcher process
	// that competes for the WS host port deciding who is told about Studio's
	// tools. A short cache keeps the badge live without putting a running agent's
	// connection at risk.
	studioStatus := cachedStudioStatus(func(ctx context.Context, projectID string) (api.StudioStatus, error) {
		placeName := ""
		if projectID != "" {
			if project, err := store.Project(ctx, projectID); err == nil {
				placeName = studio.PlaceName(project.Name, project.ID)
			}
		}
		status, err := studioProvisioner.Status(ctx, placeName)
		return api.StudioStatus{Open: status.Open, Matched: status.Matched, Blocked: status.Blocked}, err
	})
	schedulerManager.SetMCPProvisioner(func(ctx context.Context, j *scheduler.Job) scheduler.MCPGrant {
		if j.Provider != "claude" {
			return scheduler.MCPGrant{}
		}
		grant := studioProvisioner.Provision(ctx, j.RunID, j.PermissionProfile, studioTarget(ctx, j.ProjectID))
		return scheduler.MCPGrant{ConfigPath: grant.ConfigPath, AllowedTools: grant.AllowedTools, Notice: grant.Notice, Context: grant.Context, Release: grant.Release}
	})
	// The validation loop opens its own Studio MCP connection independent of
	// the run's own (already-exited, by this point) agent connection — the
	// same pattern studioProvisioner.Provision/Status already use to probe
	// Studio from the daemon's side.
	schedulerManager.SetMCPValidator(func(ctx context.Context, j *scheduler.Job) scheduler.ValidationResult {
		window := time.Duration(playtestWindowSeconds.Load()) * time.Second
		result := studioProvisioner.Validate(ctx, mcp.ValidateRequest{Target: studioTarget(ctx, j.ProjectID), Window: window})
		return scheduler.ValidationResult{Outcome: scheduler.ValidationOutcome(result.Outcome), Console: result.Console, Errors: result.Errors, Screenshot: result.Screenshot, Notice: result.Notice}
	})

	applySetting := func(key, value string) error {
		switch key {
		case "codex_path":
			codexProvider.SetExecutable(value)
		case "claude_path":
			claudeProvider.SetExecutable(value)
		case "rojo_path":
			rojoManager.SetExecutable(value)
		case "git_path":
			doctor.SetGitOverride(value)
		case "studio_mcp_path":
			doctor.SetMCPOverride(value)
			studioMCPOverride.Store(value)
		case "studio_auto_open":
			studioAutoOpen.Store(value != "false")
		case "playtest_window_seconds":
			seconds, err := strconv.Atoi(value)
			if err != nil || seconds <= 0 {
				return errors.New("playtest_window_seconds must be a positive integer")
			}
			playtestWindowSeconds.Store(int64(seconds))
		case "concurrency":
			count, err := strconv.Atoi(value)
			if err != nil {
				return err
			}
			schedulerManager.SetLimits(count, 0, 0, 0)
		}
		return nil
	}
	sessions, err := api.NewSessionManager(24 * time.Hour)
	if err != nil {
		return fmt.Errorf("create local session: %w", err)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(opts.Host, fmt.Sprint(opts.Port)))
	if err != nil {
		return fmt.Errorf("listen on %s:%d: %w", opts.Host, opts.Port, err)
	}
	defer listener.Close()
	baseURL := url.URL{Scheme: "http", Host: listener.Addr().String()}
	apiServer, err := api.New(api.Dependencies{Store: store, DB: db, Scheduler: schedulerManager, Hub: hub, Doctor: doctor, Sessions: sessions, Guard: guard, SafeMode: opts.SafeMode, AllowedHost: listener.Addr().String(), DataDir: dataDir, Logger: slog.Default(), ApplySetting: applySetting, Studio: studioOpener, StudioStatus: studioStatus, Sync: syncer, Diff: differ, Memory: memoryStore})
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: apiServer.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	launchURL := baseURL.String() + "/#bootstrap=" + url.QueryEscape(sessions.BootstrapToken())
	slog.Info("StudioForge ready", "url", baseURL.String(), "data_dir", dataDir, "safe_mode", opts.SafeMode, "mock_mode", opts.MockMode)
	fmt.Printf("STUDIOFORGE_URL=%s\nSTUDIOFORGE_BOOTSTRAP=%s\n", baseURL.String(), sessions.BootstrapToken())
	if !opts.NoOpen {
		if err := platform.OpenBrowser(launchURL); err != nil {
			slog.Warn("browser did not open automatically", "error", err, "url", launchURL)
		}
	}
	serveErr := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()
	var runErr error
	select {
	case err := <-serveErr:
		if err != nil {
			slog.Error("HTTP server stopped", "error", err)
		}
		runErr = err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	_ = schedulerManager.Close(shutdownCtx)
	_ = supervisor.Close(shutdownCtx)
	_ = db.Checkpoint(shutdownCtx)
	return runErr
}

func automaticBackup(ctx context.Context, db *database.DB, store *database.Store, dataDir string) error {
	last, ok, _ := store.Setting(ctx, "last_backup")
	if ok {
		if t, err := time.Parse(time.RFC3339Nano, last); err == nil && time.Since(t) < 24*time.Hour {
			return nil
		}
	}
	target := filepath.Join(dataDir, "backups", "automatic-"+time.Now().UTC().Format("20060102-150405")+".db")
	if _, err := os.Stat(db.Path); err != nil {
		return nil
	}
	if err := db.Backup(ctx, target); err != nil {
		return err
	}
	return store.SetSetting(ctx, "last_backup", time.Now().UTC().Format(time.RFC3339Nano))
}
