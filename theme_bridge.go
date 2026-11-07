package main

/*
#include "native.h"
*/
import "C"
import (
	"fmt"
	"os"
	"path/filepath"
	"github.com/sixuan-ai/sixuan-ime/theme"
	"strconv"
	"strings"
	"unsafe"
)

func applyAppearance(a theme.Appearance) {
	a, _ = theme.Normalize(a)
	values := []string{theme.Keyfile(a), theme.CSS(a, "settings"), theme.CSS(a, "overlay")}
	ptr := make([]*C.char, 3)
	for i, v := range values {
		ptr[i] = C.CString(v)
		defer C.free(unsafe.Pointer(ptr[i]))
	}
	C.native_theme_set(ptr[0], ptr[1], ptr[2])
	if e := theme.AtomicWrite(filepath.Join(stateHome(), stateDirName, "theme", "candidates.css"), []byte(theme.CSS(a, "candidates"))); e != nil {
		fmt.Fprintln(os.Stderr, "外观配置警告：无法写入候选样式：", e)
	}
}
func setAppearance(k, v string) error {
	if app == nil {
		return fmt.Errorf("设置尚未初始化")
	}
	a := app.config.Appearance.Editable()
	switch k {
	case "preset":
		p, ok := theme.Find(v)
		if !ok {
			return fmt.Errorf("未知皮肤")
		}
		a.Preset = p.ID
		a.Accent2 = ""
		a.Accent = p.Accent
		a.Base = p.Base
	case "accent":
		a.Accent2 = ""
		a.Accent = v
		a.Preset = "custom"
	case "base":
		if p, ok := theme.Find(a.Preset); ok {
			a.Accent2 = p.Accent2
		}
		a.Base = v
		a.Preset = "custom"
	case "radius":
		a.Radius = v
	case "glow":
		a.Glow = v
	case "candidate_size":
		a.CandidateSize = v
	case "opacity":
		value, e := strconv.ParseFloat(v, 64)
		if e != nil {
			return fmt.Errorf("不透明度无效")
		}
		a.Opacity = value
	case "reset":
		a = theme.Default()
	default:
		return fmt.Errorf("未知外观选项")
	}
	a, w := theme.Normalize(a)
	if len(w) > 0 {
		return fmt.Errorf("%s", strings.Join(w, "；"))
	}
	if e := editConfig(map[string]any{"appearance": a}); e != nil {
		return e
	}
	reloadPreferences()
	return nil
}

//export goSetAppearance
func goSetAppearance(key, value *C.char) *C.char {
	if e := setAppearance(C.GoString(key), C.GoString(value)); e != nil {
		return C.CString(e.Error())
	}
	return C.CString("")
}

func reportAppearanceWarning(c config) {
	if c.appearanceWarning != "" {
		fmt.Fprintln(os.Stderr, "外观配置警告：", c.appearanceWarning)
	}
}
