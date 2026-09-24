// SPDX-License-Identifier: Apache-2.0

// Command livesubs is the Live Subtitles server: a single binary that serves
// the web app, the HTTP API and the caption WebSockets.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	// The HTTP server lands in P0-03.
	fmt.Fprintln(os.Stderr, "livesubs", version, "- server not implemented yet")
}
