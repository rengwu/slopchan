// Package web contains the web UI bundled with the executable.
package web

import "embed"

// Files contains the templates, stylesheet, and background image.
//
//go:embed *.html *.css *.jpg
var Files embed.FS
