package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"github.com/sixuan-ai/sixuan-ime/theme"
	"testing"
)

func TestAppearanceConfigIsolation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	p := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), configDirName, "config.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	for _, appearance := range []string{`null`, `42`, `[]`, `{"accent":false,"opacity":"wrong"}`, `{"accent":"#bad","glow":"extreme","opacity":1.1}`} {
		data := `{"source":"test-mic","text_polish":"basic","appearance":` + appearance + `}`
		os.WriteFile(p, []byte(data), 0600)
		c := readConfig()
		if c.Source != "test-mic" || c.TextPolish != "basic" {
			t.Fatal("appearance rejected unrelated settings", c.Source, c.TextPolish)
		}
		if c.appearanceWarning == "" {
			t.Fatal("missing warning", appearance)
		}
		if !reflect.DeepEqual(theme.Derive(c.Appearance), theme.Derive(theme.Default())) {
			t.Fatal(c.Appearance)
		}
	}
	os.WriteFile(p, []byte(`{"source":"test-mic"}`), 0600)
	c := readConfig()
	if c.appearanceWarning != "" || !reflect.DeepEqual(theme.Derive(c.Appearance), theme.Derive(theme.Default())) {
		t.Fatal(c.Appearance, c.appearanceWarning)
	}
	a := theme.Default()
	a.Preset = "custom"
	a.Accent = "#123456"
	if e := editConfig(map[string]any{"appearance": a}); e != nil {
		t.Fatal(e)
	}
	c = readConfig()
	if c.Appearance.Accent != "#123456" || c.Source != "test-mic" {
		t.Fatal(c.Appearance, c.Source)
	}
	b, _ := os.ReadFile(p)
	var raw map[string]json.RawMessage
	json.Unmarshal(b, &raw)
	if len(raw) != 2 {
		t.Fatal("lost unrelated config")
	}
}

func TestAppearanceSaveAndHotReload(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if e := editConfig(map[string]any{"source": "unchanged-mic", "appearance": map[string]any{"glow": "invalid"}}); e != nil {
		t.Fatal(e)
	}
	previous := app
	app = &application{config: readConfig()}
	defer func() { app = previous }()
	if app.config.appearanceWarning == "" {
		t.Fatal("expected warning")
	}
	path := filepath.Join(stateHome(), stateDirName, "theme", "candidates.css")
	for _, change := range [][2]string{{"preset", "cyber"}, {"accent", "#334455"}, {"base", "#F0F0F0"}, {"radius", "square"}, {"glow", "strong"}, {"candidate_size", "large"}, {"opacity", "0.7"}, {"reset", ""}} {
		if e := setAppearance(change[0], change[1]); e != nil {
			t.Fatal(change, e)
		}
		if app.config.Source != "unchanged-mic" || app.config.appearanceWarning != "" {
			t.Fatal("unrelated config changed or stale warning")
		}
		css, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if string(css) != theme.CSS(app.config.Appearance, "candidates") {
			t.Fatal("Reload did not write current theme")
		}
	}
	if e := setAppearance("preset", "cyber"); e != nil {
		t.Fatal(e)
	}
	if e := setAppearance("base", "#222222"); e != nil {
		t.Fatal(e)
	}
	if c := readConfig(); c.Appearance.Preset != "custom" || theme.Derive(c.Appearance).Colors["accent-2"] != "#FF3EC8" {
		t.Fatal("custom base lost cyber secondary accent")
	}
	if e := setAppearance("accent", "#ABCDEF"); e != nil {
		t.Fatal(e)
	}
	if c := readConfig(); c.Appearance.Accent2 != "" {
		t.Fatal("custom accent must clear the preset secondary accent")
	}
	if e := setAppearance("reset", ""); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(path)
	for _, change := range [][2]string{{"preset", "bad"}, {"accent", "red"}, {"radius", "bad"}, {"glow", "bad"}, {"opacity", "0.8oops"}, {"opacity", "NaN"}, {"unknown", ""}} {
		if e := setAppearance(change[0], change[1]); e == nil {
			t.Fatal("invalid setting accepted", change)
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid settings changed applied CSS")
	}
	if !reflect.DeepEqual(theme.Derive(app.config.Appearance), theme.Derive(theme.Default())) {
		t.Fatal("reset")
	}
}
