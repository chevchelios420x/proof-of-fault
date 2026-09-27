//go:build windows

package power

import (
	"runtime"

	"golang.org/x/sys/windows"
)

var procSetState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

type winInhibitor struct{ done chan struct{} }

// SetThreadExecutionState is per thread, so a locked goroutine holds it.
func keepAwake() Inhibitor {
	in := &winInhibitor{done: make(chan struct{})}
	started := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		procSetState.Call(uintptr(esContinuous | esSystemRequired))
		close(started)
		<-in.done
		procSetState.Call(uintptr(esContinuous))
	}()
	<-started
	return in
}

func (w *winInhibitor) Release() { close(w.done) }
