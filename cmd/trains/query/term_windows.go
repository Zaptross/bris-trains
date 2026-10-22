//go:build windows

package query

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode = kernel32.NewProc("GetConsoleMode")
	setConsoleMode = kernel32.NewProc("SetConsoleMode")
)

const enableVirtualTerminalProcessing = 0x0004

// terminalColor reports whether w is a console that renders ANSI colour,
// enabling virtual terminal processing on consoles that need it.
func terminalColor(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	h := f.Fd()
	var mode uint32
	if r, _, _ := getConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return false // not a console (a pipe or file)
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	r, _, _ := setConsoleMode.Call(h, uintptr(mode|enableVirtualTerminalProcessing))
	return r != 0
}
