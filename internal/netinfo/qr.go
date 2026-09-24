// SPDX-License-Identifier: Apache-2.0

package netinfo

import (
	"strings"

	"rsc.io/qr"
)

// TerminalQR renders text as a QR code for a terminal, two modules per
// character cell with half blocks. Colours are forced (black on white) so it
// scans on dark and light terminal themes alike.
func TerminalQR(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 2 // modules of white border; scanners need some
	n := code.Size
	dark := func(x, y int) bool {
		if x < 0 || y < 0 || x >= n || y >= n {
			return false
		}
		return code.Black(x, y)
	}
	var b strings.Builder
	for y := -quiet; y < n+quiet; y += 2 {
		b.WriteString("\x1b[30;47m") // black on white
		for x := -quiet; x < n+quiet; x++ {
			top, bottom := dark(x, y), dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}
