// Package web holds the show's static files.
package web

import "embed"

// Files are the page, styles and scripts served at /.
//
//go:embed *.html *.css *.mjs *.svg *.png
var Files embed.FS
