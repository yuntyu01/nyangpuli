// Package arcanite embeds upstream MIT-licensed texts only; no images. See LICENSE.
package arcanite

import "embed"

const Commit = "c025b97c49e564e16019cafba381375fd1697a17"

//go:embed cards/*.json tarot-spreads.json upstream-files.json
var Files embed.FS
