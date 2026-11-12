// Package docs exists only to embed the documentation markdown in the Lewbox
// binary for serving by the web console. It has no other code.
package docs

import "embed"

// FS holds the two documentation books under docs/, each rooted at its own
// folder: consumer/ and platform/.
//
//go:embed consumer/*.md consumer/*/*.md platform/*.md platform/*/*.md
var FS embed.FS
