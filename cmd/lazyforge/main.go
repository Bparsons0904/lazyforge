// Command lazyforge is a keyboard-driven terminal UI for git forges.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is overridden at build time with -ldflags "-X main.version=<v>".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	fmt.Fprintln(os.Stderr, "lazyforge: the UI is not built yet; see docs/design.md")
	os.Exit(1)
}
