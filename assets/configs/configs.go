package configs

import "embed"

// FS holds embedded prompt files.
//go:embed *.toml
var FS embed.FS
