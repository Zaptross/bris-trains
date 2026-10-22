// Package web exposes the built Vite UI for embedding in the trains binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built UI rooted at dist/.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed directive guarantees dist exists
	}
	return sub
}

// Built reports whether a UI build is present, as opposed to only the
// committed dist/.gitkeep placeholder.
func Built() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
