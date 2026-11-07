package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSixuanDefaultURL(t *testing.T) {
	t.Setenv("SIXUAN_ASR_API_KEY", "")
	for _, url := range []string{"", " \t", "wss://custom.example.invalid/asr"} {
		c := config{ASR: asrConfig{Provider: "sixuan", URL: url}}
		validateASR(&c)
		want := url
		if strings.TrimSpace(want) == "" {
			want = "wss://sixuanai.com/v1/asr/stream"
		}
		if c.configError != "" || c.ASR.URL != want {
			t.Fatalf("sixuan URL = %q, error = %q", c.ASR.URL, c.configError)
		}
	}
	for _, provider := range []string{"funasr", "openai"} {
		for _, url := range []string{"", " \t"} {
			c := config{ASR: asrConfig{Provider: provider, URL: url, Model: "model"}}
			validateASR(&c)
			if c.configError == "" || c.ASR.URL != url {
				t.Fatalf("missing %s URL accepted or changed", provider)
			}
		}
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if c := readConfig(); !publicBuild && (c.ASR.Provider != "legacy" || c.ASR.URL != "" || c.configError != "") {
		t.Fatal("private default changed")
	}
}

func TestConfigureSixuanWithoutURLAndStatus(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SIXUAN_ASR_API_KEY", "")
	old := reloadRunningService
	reloads := 0
	reloadRunningService = func() { reloads++ }
	t.Cleanup(func() { reloadRunningService = old })
	if e := configureASR([]string{"--provider", "sixuan", "--api-key-stdin"}, strings.NewReader("dummy-default-key\n")); e != nil {
		t.Fatal(e)
	}
	c := readConfig()
	if c.configError != "" || c.ASR.URL != officialASRURL || c.ASR.APIKey != "dummy-default-key" || reloads != 1 {
		t.Fatal("default config did not roundtrip")
	}
	for _, path := range []string{"config.json", "asr-key"} {
		st, e := os.Stat(filepath.Join(configDirectory(), path))
		if e != nil || st.Mode().Perm() != 0600 {
			t.Fatal("private file permissions")
		}
	}
	for _, url := range []string{"", "wss://custom.example.invalid/asr"} {
		// Older or hand-written config may omit URL; status must resolve it too.
		if e := editConfig(map[string]any{"asr": asrConfig{Provider: "sixuan", URL: url, APIKeyFile: c.ASR.APIKeyFile}}); e != nil {
			t.Fatal(e)
		}
		var out bytes.Buffer
		if e := asrStatus(&out); e != nil {
			t.Fatal(e)
		}
		var status struct {
			Provider, URL string
			HasKey        bool `json:"has_api_key"`
		}
		if e := json.Unmarshal(out.Bytes(), &status); e != nil {
			t.Fatal(e)
		}
		want := url
		if want == "" {
			want = officialASRURL
		}
		if status.Provider != "sixuan" || status.URL != want || !status.HasKey || strings.Contains(out.String(), "dummy-default-key") {
			t.Fatal("incorrect effective status or key disclosure")
		}
	}
}

func TestASRNotificationErrorSelection(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &asrServiceError{code: "unauthorized", message: "detail"})
	if got := asrNotificationMessage(err); got != "API Key 无效或已撤销，请在 sixuanai.com 账号页重新创建" {
		t.Fatal(got)
	}
	for _, err := range []error{errors.New("quota_exceeded"), &asrServiceError{code: "internal", message: "unavailable"}} {
		if got := asrNotificationMessage(err); got != "语音识别失败："+err.Error() {
			t.Fatal("unrelated errors changed", got)
		}
	}
}
