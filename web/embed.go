// SPDX-License-Identifier: Apache-2.0

// Package web embeds the built web app (web/dist, produced by the Vite build).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built app rooted at dist/. Before the first web build it
// holds only a .gitkeep, and the server serves a placeholder page.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a valid embedded path
	}
	return sub
}
