//go:build !windows

package query

import (
	"io"
	"os"
)

// terminalColor reports whether w is a terminal.
func terminalColor(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
