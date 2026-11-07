package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicProviders(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SIXUAN_ASR_API_KEY", "")
	for _, provider := range []string{"sixuan", "funasr", "openai", "legacy", "unknown", ""} {
		c := config{ASR: asrConfig{Provider: provider, URL: "http://example.invalid", Model: "model"}}
		validateASR(&c)
		valid := provider == "sixuan" || provider == "funasr" || provider == "openai"
		if (c.configError == "") != valid {
			t.Fatalf("provider %q validation: %s", provider, c.configError)
		}
	}
	c := readConfig()
	if c.configError != configureASRMessage || !c.PinyinEnabled {
		t.Fatal("unconfigured voice must leave local pinyin usable")
	}
	if c.configError != "请先配置语音识别：登录 sixuanai.com 账号页创建 API Key，然后运行 sixuan-ime asr config --provider sixuan --api-key-stdin" {
		t.Fatal("missing official service guidance")
	}
	if e := editConfig(map[string]any{"cloud_candidates": true, "dictionary_sync": true}); e != nil {
		t.Fatal(e)
	}
	c = readConfig()
	if !c.CloudCandidates || c.DictionarySync {
		t.Fatal("cloud candidates should be configurable; personal dictionary sync must stay disabled")
	}
}
func TestPublicASRConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SIXUAN_ASR_API_KEY", "")
	old := reloadRunningService
	calls := 0
	reloadRunningService = func() { calls++ }
	t.Cleanup(func() { reloadRunningService = old })
	dir := configDirectory()
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "config.json")
	if e := os.WriteFile(p, []byte(`{"unknown":{"nested":[1,2]},"pinyin_enabled":false}`), 0644); e != nil {
		t.Fatal(e)
	}
	args := []string{"--provider", "sixuan", "--url", "wss://asr.example.invalid/v1/asr/stream", "--api-key-stdin", "--hotword", "思玄", "--hotword", "输入法"}
	if e := configureASR(args, strings.NewReader("test-secret\n")); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{dir, p, filepath.Join(dir, "asr-key")} {
		st, e := os.Stat(path)
		if e != nil {
			t.Fatal(e)
		}
		want := os.FileMode(0600)
		if st.IsDir() {
			want = 0700
		}
		if st.Mode().Perm() != want {
			t.Fatalf("wrong permissions: %s", path)
		}
	}
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte("test-secret")) {
		t.Fatal("key leaked to config")
	}
	doc, e := readConfigDocument()
	if e != nil {
		t.Fatal(e)
	}
	var unknown map[string][]int
	if json.Unmarshal(doc["unknown"], &unknown) != nil || len(unknown["nested"]) != 2 || unknown["nested"][0] != 1 || unknown["nested"][1] != 2 || string(doc["pinyin_enabled"]) != "false" {
		t.Fatal("other fields changed")
	}

	c := readConfig()
	if c.configError != "" || len(c.ASR.Hotwords) != 2 || c.ASR.APIKey != "test-secret" {
		t.Fatal("config did not roundtrip")
	}
	var out bytes.Buffer
	if e := asrStatus(&out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "test-secret") || !strings.Contains(out.String(), `"has_api_key":true`) {
		t.Fatal("status exposes key or misses presence")
	}
	if calls != 1 {
		t.Fatal("reload missing")
	}
	before := append([]byte{}, b...)
	for _, a := range [][]string{{"--provider", "legacy"}, {"--provider", "openai", "--model", ""}, {"--api-key-stdin"}, {"--unknown"}} {
		if e := configureASR(a, strings.NewReader("")); e == nil {
			t.Fatal("invalid config accepted")
		}
		after, _ := os.ReadFile(p)
		if !bytes.Equal(after, before) {
			t.Fatal("invalid update changed config")
		}
	}
	if e := configureASR([]string{"--model", "new"}, strings.NewReader("")); e != nil {
		t.Fatal(e)
	}
	c = readConfig()
	if c.ASR.Provider != "sixuan" || c.ASR.APIKey != "test-secret" || c.ASR.Model != "new" {
		t.Fatal("partial update lost settings")
	}
	if e := os.WriteFile(p, []byte("{broken"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := configureASR(args, strings.NewReader("replacement")); e == nil {
		t.Fatal("overwrote invalid config")
	}
	key, _ := os.ReadFile(filepath.Join(dir, "asr-key"))
	if string(key) != "test-secret" {
		t.Fatal("invalid config changed key")
	}
}
func TestPublicInputSourceAppend(t *testing.T) {
	for _, v := range []string{"[]", "@a(ss) []", "[('xkb', 'us')]", "[('xkb', 'us'), ('ibus', 'other')]", "[(\"ibus\", \"sixuan-pinyin\"), ('xkb','us')]"} {
		got, e := appendInputSource(v, pinyinEngineID)
		if e != nil {
			t.Fatal(e)
		}
		twice, e := appendInputSource(got, pinyinEngineID)
		if e != nil || got != twice {
			t.Fatal("not idempotent")
		}
		if strings.Contains(v, "'us'") && !strings.Contains(got, "'us'") {
			t.Fatal("removed input source")
		}
	}
	if _, e := appendInputSource("garbage", pinyinEngineID); e == nil {
		t.Fatal("accepted invalid GVariant")
	}
}
func TestPublicSetupMocked(t *testing.T) {
	root := t.TempDir()
	output, e := os.CreateTemp(root, "setup-output-")
	if e != nil {
		t.Fatal(e)
	}
	previousOutput := os.Stdout
	os.Stdout = output
	t.Cleanup(func() { os.Stdout = previousOutput; output.Close() })
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	old := runDesktopCommand
	t.Cleanup(func() { runDesktopCommand = old })
	sources := "[('xkb', 'us'), ('ibus', 'other')]"
	extensions := "['another@example.invalid']"
	serviceCalls := 0
	setCalls := 0
	runDesktopCommand = func(name string, args ...string) (string, error) {
		if name == "systemctl" {
			if strings.Join(args, " ") != "--user enable --now sixuan-ime.service" {
				t.Fatal(args)
			}
			serviceCalls++
			return "", nil
		}
		if name != "gsettings" {
			t.Fatalf("unexpected command %s", name)
		}
		switch strings.Join(args[:3], " ") {
		case "get org.gnome.desktop.input-sources sources":
			return sources, nil
		case "get org.gnome.shell enabled-extensions":
			return extensions, nil
		case "set org.gnome.desktop.input-sources sources":
			sources = args[3]
			setCalls++
		case "set org.gnome.shell enabled-extensions":
			extensions = args[3]
			setCalls++
		default:
			t.Fatal(args)
		}
		return "", nil
	}
	if e := setupDesktop(); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(configDirectory(), "config.json")
	before, _ := os.ReadFile(p)
	if e := setupDesktop(); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) || serviceCalls != 2 || setCalls != 2 {
		t.Fatal("setup not idempotent")
	}
	if !strings.Contains(sources, "'other'") || !strings.Contains(extensions, "another@example.invalid") {
		t.Fatal("lost user configuration")
	}
	if _, e := os.Stat(filepath.Join(root, "data/applications/sixuan-ime.desktop")); e != nil {
		t.Fatal(e)
	}
	printed, e := os.ReadFile(output.Name())
	if e != nil || !bytes.Contains(printed, []byte("登录 sixuanai.com 账号页创建 API Key，然后运行 sixuan-ime asr config --provider sixuan --api-key-stdin")) {
		t.Fatal("setup omitted voice configuration instructions")
	}
}

func TestPublicIBusKeys(t *testing.T) {
	previousApp, previousHandler := app, inputKeyHandler
	t.Cleanup(func() { app = previousApp; inputKeyHandler = previousHandler })
	calls := 0
	inputKeyHandler = func(device string, code, value int) {
		if device != "ibus" || code != 29 || value != 1 {
			t.Fatal("wrong key event")
		}
		calls++
	}
	app = &application{phase: "idle", config: config{HoldCtrl: true}}
	if goInputKey(29, 1) != 0 || calls != 1 {
		t.Fatal("IBus key not forwarded")
	}
	app.locked = true
	goInputKey(29, 1)
	app.locked = false
	app.config.HoldCtrl = false
	goInputKey(29, 1)
	app = nil
	goInputKey(29, 1)
	if calls != 1 {
		t.Fatal("disabled key handler ran")
	}
}
