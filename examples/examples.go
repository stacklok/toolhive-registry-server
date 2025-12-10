package examples

import "embed"

// ConfigFS embeds all config-*.yaml example files.
// This filesystem can be used by tests to validate configuration files.
//
//go:embed config-*.yaml
var ConfigFS embed.FS
