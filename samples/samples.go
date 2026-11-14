// Package samples holds raw probe output captured from (or written for) real
// systems. Tests parse them; --demo replays them as fake machines.
package samples

import "embed"

// FS contains <system>/detail.txt for every sample.
//
//go:embed */detail.txt
var FS embed.FS
