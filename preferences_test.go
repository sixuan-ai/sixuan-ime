package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPreferencesValidateMergeAndRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if e := editConfig(map[string]any{"source": "test-mic", "ctrl_hold_ms": 650, "dictionary_sync": false}); e != nil {
		t.Fatal(e)
	}
	for value, mode := range []string{"off", "basic", "aggressive"} {
		if e := setPreference("text_polish", value); e != nil {
			t.Fatal(e)
		}
		c := readConfig()
		if c.TextPolish != mode || c.Source != "test-mic" || c.HoldMS != 650 {
			t.Fatal(c.TextPolish, c.Source, c.HoldMS)
		}
	}
	for _, key := range []string{"cloud_candidates", "pinyin_enabled", "sound_feedback"} {
		for _, v := range []int{0, 1} {
			if e := setPreference(key, v); e != nil {
				t.Fatal(e)
			}
			if publicBuild && key == "cloud_candidates" {
				continue
			}
			if preferenceValue(readConfig(), key) != v {
				t.Fatal(key, v)
			}
		}
	}
	for _, entry := range []struct {
		key   string
		value int
	}{{"text_polish", 3}, {"text_polish", -1}, {"cloud_candidates", 2}, {"source", 0}, {"dictionary_sync", 1}} {
		if e := setPreference(entry.key, entry.value); e == nil {
			t.Fatal("invalid setting accepted", entry)
		}
	}
	p := filepath.Join(dir, configDirName+"/config.json")
	before, _ := os.ReadFile(p)
	var saved map[string]any
	if json.Unmarshal(before, &saved) != nil {
		t.Fatal("bad json")
	}
	if saved["dictionary_sync"] != false {
		t.Fatal("changed pairing config")
	}
	os.WriteFile(p, []byte("{broken"), 0600)
	if setPreference("text_polish", 0) == nil {
		t.Fatal("overwrote invalid config")
	}
	after, _ := os.ReadFile(p)
	if string(after) != "{broken" {
		t.Fatal("modified invalid config")
	}
}
