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
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitea"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui"
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

	if demo {
		return runUI(ctx, core.New(forgetest.NewDemo(time.Now()), core.Options{}))
	}
	svc, err := realService(ctx, configPath, hostName, noUpdateCheck)
	if err != nil {
		return err
	}
	return runUI(ctx, svc)
}

func runUI(ctx context.Context, svc *core.Service) error {
	_, err := tea.NewProgram(ui.New(ctx, svc)).Run()
	return err
}

func realService(ctx context.Context, configPath, hostName string, noUpdateCheck bool) (*core.Service, error) {
	if configPath == "" {
		paths, err := config.DefaultPaths()
		if err != nil {
			return nil, err
		}
		configPath = paths.Config
	}
	cfg, err := config.Load(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		maybeUpdate(ctx, !noUpdateCheck) // a missing config means the check is on
		return nil, errors.New("no config yet: onboarding is not built yet (#24)")
	}
	if err != nil {
		return nil, err
	}
	maybeUpdate(ctx, !noUpdateCheck && cfg.Update.Check)
	name, err := config.SelectHost(cfg, hostName)
	if errors.Is(err, config.ErrNeedPicker) {
		names := make([]string, 0, len(cfg.Hosts))
		for n := range cfg.Hosts {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("pick a host with --host (configured: %s)", strings.Join(names, ", "))
	}
	if err != nil {
		return nil, err
	}
	h := cfg.Hosts[name]
	if h.Type != "forgejo" && h.Type != "gitea" {
		return nil, fmt.Errorf("host %q: type %q has no adapter yet", name, h.Type)
	}
	token, err := config.ResolveToken(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("host %q: %w", name, err)
	}
	f, err := gitea.New(ctx, h.URL, token, &http.Client{Timeout: 30 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("host %q: %w", name, err)
	}
	greenOnly := func(r domain.RepoRef) bool { return h.RequiresGreenCI(r.String()) }
	return core.New(f, core.Options{RequireGreenCI: greenOnly}), nil
}
