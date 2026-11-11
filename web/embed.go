// Package web exists only to embed the built web console in the Lewbox
// binary. Vite builds the React app into web/dist.
package web

import (
	"embed"
	"io/fs"
)

// Dist holds the built web console, rooted at this folder. dist keeps a
// .gitkeep so the pattern matches even before the app is built.
//
//go:embed all:dist
var Dist embed.FS

// DistFS returns the built web console, rooted at dist/.
func DistFS() fs.FS {
	sub, err := fs.Sub(Dist, "dist")
	if err != nil {
		// The embed pattern guarantees the directory exists.
		panic(err)
	}
	return sub
}
