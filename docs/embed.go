// Package docs exists only to embed the documentation markdown in the Lewbox
// binary for serving by the web console. It has no other code.
package docs

import "embed"

// FS holds every markdown page under docs/, rooted at this folder.
//
//go:embed *.md */*.md */*/*.md
var FS embed.FS
