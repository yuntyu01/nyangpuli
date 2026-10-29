// Package tarotoo embeds the upstream MIT-licensed dataset. See LICENSE.
package tarotoo

import _ "embed"

const Commit = "83202267973950eb5bf034f144b9405db9cb1e7e"

//go:embed cards.json
var Cards []byte
