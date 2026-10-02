// Command lazyforge is a keyboard-driven terminal UI for git forges.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
)

// version is overridden at build time with -ldflags "-X main.version=<v>".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	configPath := flag.String("config", "", "path to the config file (default: per-user config dir)")
	hostName := flag.String("host", "", "name of the configured host to use")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	// run only returns once the UI exists; until then it always fails.
	fmt.Fprintln(os.Stderr, "lazyforge:", run(*configPath, *hostName))
	os.Exit(1)
}

func run(configPath, hostName string) error {
	if configPath == "" {
		paths, err := config.DefaultPaths()
		if err != nil {
			return err
		}
		configPath = paths.Config
	}
	cfg, err := config.Load(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New("no config yet: onboarding is not built yet (#24)")
	}
	if err != nil {
		return err
	}
	name, err := config.SelectHost(cfg, hostName)
	if errors.Is(err, config.ErrNeedPicker) {
		names := make([]string, 0, len(cfg.Hosts))
		for n := range cfg.Hosts {
			names = append(names, n)
		}
		sort.Strings(names)
		return fmt.Errorf("pick a host with --host (configured: %s)", strings.Join(names, ", "))
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("host %q selected, but the UI is not built yet; see docs/design.md", name)
}
