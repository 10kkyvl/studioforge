package webui

import "embed"

// Files contains the production SvelteKit build. Node.js is never needed at runtime.
//
//go:embed all:dist
var Files embed.FS
