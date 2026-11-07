package main

/*
#cgo pkg-config: libpulse-simple gtk+-3.0 ibus-1.0 rime
#include "native.h"
*/
import "C"
import (
	"path/filepath"
)

func prepareASR()              {}
func enableDaemonSessions()    {}
func configureDictionary(bool) {}
func phraseFiles(dir string) []string {
	return []string{wordsPath("learned-words.json"), filepath.Join(dir, configDirName, "phrases.tsv")}
}
