package main

/*
#include "native.h"
*/
import "C"
import "unsafe"

var reloadRunningService = func() {
	p, free := cstr("Reload")
	defer free()
	r := C.native_control(p)
	C.free(unsafe.Pointer(r))
}
