package main

/*
#include "native.h"
*/
import "C"
import "errors"

func preferenceValue(c config, key string) int {
	switch key {
	case "text_polish":
		if c.TextPolish == "basic" {
			return 1
		}
		if c.TextPolish == "aggressive" {
			return 2
		}
	case "local_dictionary":
		if (c.LocalDictionary == "imported" || c.LocalDictionary == "bundled") && c.localDictionaryError == "" {
			return 1
		}
	case "cloud_candidates":
		if c.CloudCandidates {
			return 1
		}
	case "pinyin_enabled":
		if c.PinyinEnabled {
			return 1
		}
	case "sound_feedback":
		if c.Sound == nil || *c.Sound {
			return 1
		}
	}
	return 0
}
func setPreference(key string, value int) error {
	var setting any
	switch key {
	case "text_polish":
		if value < 0 || value > 2 {
			return errors.New("文字整理选项无效")
		}
		setting = []string{"off", "basic", "aggressive"}[value]
	case "local_dictionary":
		if value != 0 && value != 1 {
			return errors.New("开关选项无效")
		}
		setting = "default"
		if value == 1 {
			if _, err := loadLocalBundle(true); err == nil {
				setting = "imported"
			} else if _, err := ensureBundledDictionary(); err == nil {
				setting = "bundled"
			} else {
				return errors.New("没有可用的扩展词库，请先导入词库或重新安装内置词库")
			}
		}
	case "cloud_candidates", "pinyin_enabled", "sound_feedback":
		if value != 0 && value != 1 {
			return errors.New("开关选项无效")
		}
		setting = value == 1
	default:
		return errors.New("未知设置")
	}
	return editConfig(map[string]any{key: setting})
}

//export goPreferenceValue
func goPreferenceValue(key *C.char) C.int {
	if app == nil {
		return 0
	}
	return C.int(preferenceValue(app.config, C.GoString(key)))
}

//export goSetPreference
func goSetPreference(key *C.char, value C.int) *C.char {
	if e := setPreference(C.GoString(key), int(value)); e != nil {
		return C.CString(e.Error())
	}
	reloadPreferences()
	return C.CString("")
}
func reloadPreferences() {
	app.config = readConfig()
	applyAppearance(app.config.Appearance)
	reportAppearanceWarning(app.config)
	configureDictionary(app.config.DictionarySync)
	if !app.config.CloudCandidates || !app.config.PinyinEnabled {
		if pinyinState.cancel != nil {
			pinyinState.cancel()
			pinyinState.cancel = nil
		}
		if pinyinState.timer != nil {
			pinyinState.timer.Stop()
		}
	}
	C.native_pinyin_preferences_changed()
	C.native_settings_refresh()
}
