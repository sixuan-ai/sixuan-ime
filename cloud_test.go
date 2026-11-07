package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type candidateRoundTrip func(*http.Request) (*http.Response, error)

func (f candidateRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockCandidateHTTP(t *testing.T, f candidateRoundTrip) {
	t.Helper()
	old := candidateHTTPClient
	candidateHTTPClient = &http.Client{Transport: f, CheckRedirect: old.CheckRedirect}
	t.Cleanup(func() { candidateHTTPClient = old })
}
func TestPublicCandidateRequestAndFiltering(t *testing.T) {
	mockCandidateHTTP(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != officialCandidatesURL || r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("unexpected destination/auth")
		}
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || body["query"] != "ni hao" {
			t.Fatal("must send only pinyin", body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"candidates":[{"text":"你好","syllable":"ni hao"},{"text":"不符","syllable":"bie de"}]}`)), Header: make(http.Header)}, nil
	})
	rows, err := queryCloud(context.Background(), "ni hao", config{ASR: asrConfig{Provider: "sixuan", APIKey: "test-key"}})
	if err != nil || len(rows) != 1 || rows[0].Text != "你好" {
		t.Fatal(rows, err)
	}
}
func TestPublicCandidatesNeverSendASRKeys(t *testing.T) {
	mockCandidateHTTP(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Fatal("ASR key leaked into anonymous candidates")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"candidates":[]}`))}, nil
	})
	for _, c := range []config{{}, {ASR: asrConfig{Provider: "openai", APIKey: "third-party-key"}}, {ASR: asrConfig{Provider: "sixuan", APIKey: "voice-key"}}} {
		if _, err := queryCloud(context.Background(), "nihao", c); err != nil {
			t.Fatal(err)
		}
	}
}
func TestPublicCloudEnableWithoutKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SIXUAN_ASR_API_KEY", "")
	old := reloadRunningService
	reloadRunningService = func() {}
	t.Cleanup(func() { reloadRunningService = old })
	if err := cloudCommand([]string{"enable"}); err != nil {
		t.Fatal(err)
	}
	if c := readConfig(); !c.CloudCandidates || c.DictionarySync {
		t.Fatal("anonymous opt-in failed")
	}
}
func TestPublicCandidateErrorsAndCancellation(t *testing.T) {
	for _, status := range []int{401, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			mockCandidateHTTP(t, func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("private upstream details"))}, nil
			})
			_, err := queryCloud(context.Background(), "nihao", config{ASR: asrConfig{Provider: "sixuan", APIKey: "test-key"}})
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
		})
	}
	mockCandidateHTTP(t, func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := queryCloud(ctx, "nihao", config{ASR: asrConfig{Provider: "sixuan", APIKey: "test-key"}}); err == nil {
		t.Fatal("ignored cancellation")
	}
}
