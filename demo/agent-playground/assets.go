// Package playground embeds the Agent Playground's static assets: the web
// UI, the OpenAPI document, the default configuration and the mockagents
// fixtures. The `playground` binary is therefore self-contained and can run
// from any directory.
package playground

import "embed"

// FS holds the embedded assets.
//
//go:embed web openapi.yaml config/playground.json config/mock-pricing.yaml mockagents/*.yaml
var FS embed.FS
