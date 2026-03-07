package static

import "embed"

//go:embed *.html *.css *.js vendor
var FS embed.FS
