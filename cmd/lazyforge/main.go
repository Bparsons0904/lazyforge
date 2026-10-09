// Command lazyforge is a keyboard-driven terminal UI for git forges.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitea"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

// version is overridden at build time with -ldflags "-X main.version=<v>".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	configPath := flag.String("config", "", "path to the config file (default: per-user config dir)")
	hostName := flag.String("host", "", "name of the configured host to use")
	noUpdateCheck := flag.Bool("no-update-check", false, "skip the startup check for a newer release")
	demo := flag.Bool("demo", false, "run on built-in demo data; ignores --config and --host")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if err := run(*configPath, *hostName, *noUpdateCheck, *demo); err != nil {
		fmt.Fprintln(os.Stderr, "lazyforge:", err)
		os.Exit(1)
	}
}

func run(configPath, hostName string, noUpdateCheck, demo bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var root tea.Model
	if demo {
		root = ui.New(ctx, core.New(forgetest.NewDemo(time.Now()), core.Options{}))
	} else {
		d, err := appDeps(ctx, configPath, hostName, noUpdateCheck)
		if err != nil {
			return err
		}
		root = ui.NewApp(ctx, d)
	}
	_, err := tea.NewProgram(root).Run()
	return err
}

// appDeps leaves Host empty when the UI must show the picker, and sets Fresh when there is no config yet.
func appDeps(ctx context.Context, configPath, hostName string, noUpdateCheck bool) (ui.Deps, error) {
	paths, pathsErr := config.DefaultPaths()
	if configPath == "" {
		if pathsErr != nil {
			return ui.Deps{}, pathsErr
		}
		configPath = paths.Config
	}
	// State is disposable: with an explicit --config, a missing home dir just means no remembered host.
	var statePath string
	var state config.State
	if pathsErr == nil {
		statePath = paths.State
		state, _ = config.LoadState(statePath)
	}
	d := ui.Deps{
		ConfigPath: configPath,
		StatePath:  statePath,
		LastHost:   state.LastHost,
		Connect:    connect,
		Probe: func(ctx context.Context, url string) (forge.Kind, error) {
			return gitea.Probe(ctx, url, &http.Client{Timeout: 10 * time.Second})
		},
		Detect: termimg.NewDetector(termimg.SystemProbes(), time.Second),
	}
	cfg, err := config.Load(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		maybeUpdate(ctx, !noUpdateCheck) // a missing config means the check is on
		d.Fresh = true
		d.Config = config.Defaults()
		return d, nil
	}
	if err != nil {
		return ui.Deps{}, err
	}
	d.Config = cfg
	maybeUpdate(ctx, !noUpdateCheck && cfg.Update.Check)
	name, err := config.SelectHost(cfg, hostName)
	if errors.Is(err, config.ErrNeedPicker) {
		return d, nil
	}
	if err != nil {
		return ui.Deps{}, err
	}
	f, err := connect(ctx, cfg.Hosts[name])
	if err != nil {
		return ui.Deps{}, fmt.Errorf("host %q: %w", name, err)
	}
	d.Host, d.Forge = name, f
	return d, nil
}

// connect errors carry no host name; callers add it.
func connect(ctx context.Context, h config.Host) (forge.Forge, error) {
	if h.Type != "forgejo" && h.Type != "gitea" {
		return nil, fmt.Errorf("type %q has no adapter yet", h.Type)
	}
	token, err := config.ResolveToken(ctx, h)
	if err != nil {
		return nil, err
	}
	return gitea.New(ctx, h.URL, token, &http.Client{Timeout: 30 * time.Second})
}
