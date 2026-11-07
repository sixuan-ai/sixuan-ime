package main

/*
#include "native.h"
*/
import "C"

var inputKeyHandler = handleKey

// The public engine receives keys from IBus, so setup needs no access to raw
// keyboard devices. The private build retains its existing evdev path.
//
//export goInputKey
func goInputKey(code, value C.int) C.int {
	if !publicBuild || app == nil || app.locked || !app.config.HoldCtrl {
		return 0
	}
	if value == 1 && app.phase != "idle" {
		if code == 1 {
			app.action("cancel")
			return 1
		}
		if code == 57 && app.phase == "recording" {
			app.action("stop")
			return 1
		}
	}
	inputKeyHandler("ibus", int(code), int(value))
	return 0
}
