// Package std embeds the Cero standard library (ADR-0011).
package std

import "embed"

// FS holds list.cero, option.cero, pair.cero and string.cero.
//
//go:embed *.cero
var FS embed.FS
