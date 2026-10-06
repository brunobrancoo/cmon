package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework Carbon
#include <stdlib.h>
#include "ui.h"
*/
import "C"

import "unsafe"

// uiRun runs the Cocoa event loop. Must be called from the main thread.
func uiRun() { C.cmon_run() }

// uiUpdate hands a snapshot (JSON) to the window. Safe from any goroutine.
func uiUpdate(snapshot string) {
	cs := C.CString(snapshot)
	defer C.free(unsafe.Pointer(cs))
	C.cmon_update(cs)
}

// uiPlay plays a macOS system sound by name (/System/Library/Sounds).
func uiPlay(name string) {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	C.cmon_play(cs)
}
