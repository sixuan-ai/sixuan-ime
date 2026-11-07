package main

/*
#include "native.h"
*/
import "C"

//export goPublicBuild
func goPublicBuild() C.int {
	if publicBuild {
		return 1
	}
	return 0
}

//export goBuildName
func goBuildName(index C.int) *C.char { return C.CString(buildNames[int(index)]) }
