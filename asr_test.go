package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// An httptest server over net.Pipe keeps protocol tests fully local even in
// sandboxes that prohibit listening sockets. Production dialers remain unchanged.
type memoryListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func (l *memoryListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *memoryListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *memoryListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80} }
func (l *memoryListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	a, b := net.Pipe()
	select {
	case l.connections <- a:
		return b, nil
	case <-ctx.Done():
		a.Close()
		b.Close()
		return nil, ctx.Err()
	case <-l.done:
		a.Close()
		b.Close()
		return nil, net.ErrClosed
	}
}
func memoryServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	l := &memoryListener{connections: make(chan net.Conn), done: make(chan struct{})}
	s := &httptest.Server{Listener: l, Config: &http.Server{Handler: h}}
	s.Start()
	previousWS, previousHTTP := asrWebsocketDialer, asrHTTPTransport
	d := *websocket.DefaultDialer
	d.Proxy = nil
	d.NetDialContext = l.dial
	asrWebsocketDialer = &d
	tr := &http.Transport{DialContext: l.dial}
	asrHTTPTransport = tr
	t.Cleanup(func() {
		tr.CloseIdleConnections()
		s.Close()
		asrWebsocketDialer = previousWS
		asrHTTPTransport = previousHTTP
	})
	return s
}

func testAudio(n int) <-chan []byte {
	ch := make(chan []byte, n)
	for i := 0; i < n; i++ {
		ch <- make([]byte, 640)
	}
	close(ch)
	return ch
}
func wsMock(t *testing.T, handler func(*websocket.Conn, *http.Request)) string {
	t.Helper()
	s := memoryServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			t.Error(e)
			return
		}
		defer c.Close()
		handler(c, r)
	}))
	t.Cleanup(s.Close)
	return "ws" + strings.TrimPrefix(s.URL, "http")
}
func readStart(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	var m map[string]any
	if e := c.ReadJSON(&m); e != nil {
		t.Error(e)
	}
	return m
}
func receiveStop(t *testing.T, c *websocket.Conn, sixuan bool) int {
	t.Helper()
	total := 0
	for {
		kind, b, e := c.ReadMessage()
		if e != nil {
			t.Error(e)
			return total
		}
		if kind == websocket.BinaryMessage {
			if len(b) > 3200 || len(b)%2 != 0 {
				t.Errorf("invalid chunk %d", len(b))
			}
			total += len(b)
			continue
		}
		var m map[string]any
		if e = json.Unmarshal(b, &m); e != nil {
			t.Error(e)
		}
		if sixuan && m["type"] != "stop" || !sixuan && m["is_speaking"] != false {
			t.Errorf("bad stop %v", m)
		}
		return total
	}
}
func TestSixuan(t *testing.T) {
	url := wsMock(t, func(c *websocket.Conn, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-key" {
			t.Error("authorization")
		}
		m := readStart(t, c)
		polishMode := "aggressive"
		if publicBuild {
			polishMode = "off"
		}
		want := map[string]any{"type": "start", "format": "pcm_s16le", "sample_rate": float64(16000), "channels": float64(1), "language": "zh", "punctuation": true, "polish": polishMode, "hotwords": []any{"思玄"}}
		if !reflect.DeepEqual(m, want) {
			t.Errorf("start %#v", m)
		}
		c.WriteJSON(map[string]any{"type": "future"})
		c.WriteJSON(map[string]any{"type": "ready"})
		for _, s := range []string{"嗯，今天", "嗯，今天", "今天天气"} {
			c.WriteJSON(map[string]any{"type": "partial", "text": s})
		}
		if n := receiveStop(t, c, true); n != (15+10)*640 {
			t.Errorf("bytes %d", n)
		}
		c.WriteJSON(map[string]any{"type": "final", "text": "嗯，最终结果"})
	})
	c := config{ASR: asrConfig{Provider: "sixuan", URL: url, APIKey: "mock-key", Hotwords: []string{"思玄"}}, TextPolish: "aggressive", TailMS: 200}
	var partials []string
	text, e := recognizeWith(context.Background(), testAudio(15), func(e event) { partials = append(partials, e.text) }, c, &websocketRecognizer{c, true})
	wantText := "嗯，最终结果"
	if publicBuild {
		wantText = "最终结果"
	}
	if e != nil || text != wantText || !reflect.DeepEqual(partials, []string{"嗯，今天", "今天天气"}) {
		t.Fatal(text, e, partials)
	}
}
func TestSixuanError(t *testing.T) {
	for _, tt := range []struct{ code, notification string }{
		{"quota_exceeded", "今日语音额度已用完，北京时间 0 点重置"},
		{"unauthorized", "API Key 无效或已撤销，请在 sixuanai.com 账号页重新创建"},
		{"unavailable", "语音识别服务暂时不可用，请稍后再试"},
		{"rate_limited", "请求过于频繁，请稍后再试"},
		{"bad_request", "语音识别失败：ASR bad_request: server detail [redacted]"},
	} {
		for _, ready := range []bool{false, true} {
			t.Run(tt.code+"/"+map[bool]string{true: "after-ready", false: "before-ready"}[ready], func(t *testing.T) {
				url := wsMock(t, func(c *websocket.Conn, _ *http.Request) {
					readStart(t, c)
					if ready {
						c.WriteJSON(map[string]any{"type": "ready"})
						receiveStop(t, c, true)
					}
					c.WriteJSON(map[string]any{"type": "error", "code": tt.code, "message": "server detail mock-key"})
				})
				r := &websocketRecognizer{config{ASR: asrConfig{URL: url, APIKey: "mock-key"}}, true}
				_, e := r.recognize(context.Background(), testAudio(15), func(string) {})
				if e == nil || !strings.Contains(e.Error(), tt.code) || strings.Contains(e.Error(), "mock-key") {
					t.Fatal(e)
				}
				if got := asrNotificationMessage(e); got != tt.notification {
					t.Fatalf("notification = %q, want %q", got, tt.notification)
				}
			})
		}
	}
}
func TestSixuanTimeoutAndCancel(t *testing.T) {
	old := sixuanFinalTimeout
	sixuanFinalTimeout = 30 * time.Millisecond
	t.Cleanup(func() { sixuanFinalTimeout = old })
	url := wsMock(t, func(c *websocket.Conn, _ *http.Request) {
		readStart(t, c)
		c.WriteJSON(map[string]any{"type": "ready"})
		for {
			if _, _, e := c.ReadMessage(); e != nil {
				return
			}
		}
	})
	r := &websocketRecognizer{config{ASR: asrConfig{URL: url}}, true}
	_, e := r.recognize(context.Background(), testAudio(15), func(string) {})
	if e == nil || !strings.Contains(e.Error(), "timed out") {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan []byte)
	done := make(chan error, 1)
	cancel()
	go func() { _, e := r.recognize(ctx, ch, func(string) {}); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
}
func TestFunASR(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{true: "fallback", false: "final"}[fallback], func(t *testing.T) {
			old, oldIdle := funasrFinalTimeout, funasrIdleTimeout
			funasrFinalTimeout = 50 * time.Millisecond
			funasrIdleTimeout = 20 * time.Millisecond
			defer func() { funasrFinalTimeout, funasrIdleTimeout = old, oldIdle }()
			url := wsMock(t, func(c *websocket.Conn, _ *http.Request) {
				m := readStart(t, c)
				if m["mode"] != "2pass" || m["audio_fs"] != float64(16000) || m["wav_format"] != "pcm" || m["itn"] != true || m["is_speaking"] != true || m["hotwords"] != "{\"思玄\":20}" {
					t.Errorf("start %v", m)
				}
				receiveStop(t, c, false)
				for _, m := range []asrMessage{{Mode: "2pass-online", Text: "在"}, {Mode: "online", Text: "线"}, {Mode: "2pass-offline", Text: "修正。"}, {Mode: "online", Text: "第二段"}, {Mode: "offline", Text: "第二段修正。"}, {Mode: "online", Text: "尾段"}} {
					c.WriteJSON(m)
				}
				if !fallback {
					c.WriteJSON(asrMessage{IsFinal: true})
					return
				}
				for {
					if _, _, e := c.ReadMessage(); e != nil {
						return
					}
				}
			})
			r := &websocketRecognizer{settings: config{ASR: asrConfig{URL: url, Hotwords: []string{"思玄"}}}}
			var parts []string
			text, e := r.recognize(context.Background(), testAudio(15), func(s string) { parts = append(parts, s) })
			if e != nil || text != "修正。第二段修正。尾段" {
				t.Fatal(text, e)
			}
			if !reflect.DeepEqual(parts[:4], []string{"在", "在线", "修正。", "修正。第二段"}) {
				t.Fatal(parts)
			}
		})
	}
}
func TestOpenAI(t *testing.T) {
	for _, status := range []int{200, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := memoryServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer mock-key" {
					t.Error("request")
				}
				if e := r.ParseMultipartForm(4 << 20); e != nil {
					t.Error(e)
					return
				}
				defer r.MultipartForm.RemoveAll()
				for k, v := range map[string]string{"model": "whisper-1", "language": "zh", "response_format": "json"} {
					if r.FormValue(k) != v {
						t.Errorf("field %s", k)
					}
				}
				f, _, e := r.FormFile("file")
				if e != nil {
					t.Error(e)
					return
				}
				defer f.Close()
				b, _ := io.ReadAll(f)
				if len(b) != 44+9600 || string(b[:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || string(b[36:40]) != "data" || binary.LittleEndian.Uint32(b[24:]) != 16000 || binary.LittleEndian.Uint16(b[22:]) != 1 || binary.LittleEndian.Uint16(b[34:]) != 16 || binary.LittleEndian.Uint32(b[40:]) != 9600 {
					t.Error("invalid WAV")
				}
				w.WriteHeader(status)
				if status == 200 {
					io.WriteString(w, `{"text":"识别"}`)
				} else {
					io.WriteString(w, "mock-key"+strings.Repeat("x", 2000))
				}
			}))
			defer s.Close()
			r := &openaiRecognizer{asrConfig{URL: s.URL + "/v1/", Model: "whisper-1", APIKey: "mock-key"}}
			text, e := r.recognize(context.Background(), testAudio(15), func(string) { t.Error("unexpected partial") })
			if status == 200 {
				if e != nil || text != "识别" {
					t.Fatal(text, e)
				}
			} else if e == nil || !strings.Contains(e.Error(), "429") || strings.Contains(e.Error(), "mock-key") || len(e.Error()) > 1100 {
				t.Fatal(e)
			}
		})
	}
}

type recognizerFunc func(context.Context, <-chan []byte, func(string)) (string, error)

func (f recognizerFunc) recognize(ctx context.Context, a <-chan []byte, p func(string)) (string, error) {
	return f(ctx, a, p)
}
func TestASRShell(t *testing.T) {
	for _, n := range []int{0, 14, 15, 3001} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			total := 0
			r := recognizerFunc(func(ctx context.Context, a <-chan []byte, p func(string)) (string, error) {
				for b := range a {
					total += len(b)
				}
				p("a")
				p("a")
				p("b")
				return "嗯，结果", nil
			})
			var parts []string
			c := config{ASR: asrConfig{Provider: "openai"}, TailMS: 200, TextPolish: "basic"}
			text, e := recognizeWith(context.Background(), testAudio(n), func(e event) { parts = append(parts, e.text) }, c, r)
			if n < 15 {
				if e == nil || e.Error() != "recording too short" {
					t.Fatal(e)
				}
			} else if n > 3000 {
				if e == nil || e.Error() != "audio too long" {
					t.Fatal(e)
				}
			} else if e != nil || text != "结果" || total != 16000 || !reflect.DeepEqual(parts, []string{"a", "b"}) {
				t.Fatal(text, e, total, parts)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := recognizeWith(ctx, make(chan []byte), func(event) {}, config{}, recognizerFunc(func(ctx context.Context, _ <-chan []byte, _ func(string)) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}))
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestASRConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SIXUAN_ASR_API_KEY", " env-test ")
	if c := readConfig(func(c *config) {
		if publicBuild {
			c.ASR.Provider = "funasr"
			c.ASR.URL = "ws://mock"
		}
	}); (!publicBuild && c.ASR.Provider != "legacy") || c.configError != "" || c.ASR.APIKey != "env-test" {
		t.Fatal("default or env")
	}
	for _, a := range []asrConfig{{Provider: "bad"}, {Provider: "funasr"}, {Provider: "openai", URL: "http://mock"}} {
		c := config{ASR: a}
		validateASR(&c)
		if c.configError == "" {
			t.Fatal(a.Provider)
		}
	}
	p := filepath.Join(t.TempDir(), "key")
	if e := os.WriteFile(p, []byte(" file-test\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []os.FileMode{0600, 0400, 0644, 0620} {
		os.Chmod(p, mode)
		c := config{ASR: asrConfig{Provider: "funasr", URL: "ws://mock", APIKeyFile: p}}
		validateASR(&c)
		if mode&0077 != 0 {
			if c.configError == "" {
				t.Fatal("wide key permissions accepted")
			}
		} else if c.configError != "" || c.ASR.APIKey != "file-test" {
			t.Fatal("file key")
		}
	}
	c := config{ASR: asrConfig{Provider: "funasr", URL: "ws://mock", APIKey: "inline"}}
	validateASR(&c)
	if c.ASR.APIKey != "inline" {
		t.Fatal("inline precedence")
	}
	c = config{ASR: asrConfig{APIKeyFile: p + "-missing"}}
	validateASR(&c)
	if c.configError == "" {
		t.Fatal("missing file")
	}
}
func TestASRCLI(t *testing.T) {
	for _, args := range [][]string{{"x.wav", "--provider", "openai", "--url=http://mock/v1", "--model", "whisper-1"}, {"--provider=openai", "--url", "http://mock/v1", "--model=whisper-1", "x.wav"}} {
		p, override, e := parseASRArgs(args)
		if e != nil || p != "x.wav" {
			t.Fatal(p, e)
		}
		c := config{}
		override(&c)
		validateASR(&c)
		if c.configError != "" || c.ASR.Provider != "openai" || c.ASR.Model != "whisper-1" {
			t.Fatal("override")
		}
	}
	for _, args := range [][]string{nil, {"a", "b"}, {"a", "--unknown=x"}, {"a", "--url"}} {
		if _, _, e := parseASRArgs(args); e == nil {
			t.Fatal(args)
		}
	}
}

func TestSixuanCancelAfterReady(t *testing.T) {
	ready := make(chan struct{})
	url := wsMock(t, func(c *websocket.Conn, _ *http.Request) {
		readStart(t, c)
		c.WriteJSON(asrMessage{Type: "ready"})
		close(ready)
		for {
			if _, _, e := c.ReadMessage(); e != nil {
				return
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	r := &websocketRecognizer{settings: config{ASR: asrConfig{URL: url}}, sixuan: true}
	go func() { _, e := r.recognize(ctx, make(chan []byte), func(string) {}); result <- e }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("no ready")
	}
	cancel()
	select {
	case e := <-result:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close connection")
	}
}
func TestSixuanReadyTimeout(t *testing.T) {
	old := sixuanReadyTimeout
	sixuanReadyTimeout = 20 * time.Millisecond
	defer func() { sixuanReadyTimeout = old }()
	url := wsMock(t, func(c *websocket.Conn, _ *http.Request) {
		readStart(t, c)
		for {
			if _, _, e := c.ReadMessage(); e != nil {
				return
			}
		}
	})
	r := &websocketRecognizer{settings: config{ASR: asrConfig{URL: url}}, sixuan: true}
	if _, e := r.recognize(context.Background(), testAudio(15), func(string) {}); e == nil || !strings.Contains(e.Error(), "ready") {
		t.Fatal(e)
	}
}
func TestASRConfigJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("SIXUAN_ASR_API_KEY", "")
	p := filepath.Join(dir, configDirName, "config.json")
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		body     string
		valid    bool
		provider string
	}{
		{`{}`, !publicBuild, "legacy"},
		{`{"asr":{"provider":"sixuan","url":"ws://mock"}}`, true, "sixuan"},
		{`{"asr":{"provider":"funasr","url":"ws://mock"}}`, true, "funasr"},
		{`{"asr":{"provider":"openai","url":"http://mock/v1","model":"whisper-1"}}`, true, "openai"},
		{`{"asr":{"provider":"unknown"}}`, false, ""},
		{`{"asr":{"provider":"sixuan"}}`, true, "sixuan"},
		{`{"asr":{"provider":"openai","url":"http://mock"}}`, false, ""},
		{`{"asr":3}`, false, ""},
	} {
		if e := os.WriteFile(p, []byte(tt.body), 0600); e != nil {
			t.Fatal(e)
		}
		c := readConfig()
		if (c.configError == "") != tt.valid || tt.valid && c.ASR.Provider != tt.provider {
			t.Fatalf("%s: %s", tt.body, c.configError)
		}
	}
}
func TestASRShellTailAndDeadline(t *testing.T) {
	input := make(chan []byte, 15)
	for i := 0; i < 15; i++ {
		b := make([]byte, 640)
		for j := range b {
			b[j] = 1
		}
		input <- b
	}
	close(input)
	r := recognizerFunc(func(_ context.Context, a <-chan []byte, _ func(string)) (string, error) {
		i := 0
		for b := range a {
			for _, v := range b {
				if i < 15 && v != 1 || i >= 15 && v != 0 {
					t.Error("tail content")
				}
			}
			i++
		}
		if i != 25 {
			t.Errorf("frames %d", i)
		}
		return "result", nil
	})
	if _, e := recognizeWith(context.Background(), input, func(event) {}, config{TailMS: 200}, r); e != nil {
		t.Fatal(e)
	}
	old := asrTimeout
	asrTimeout = 20 * time.Millisecond
	defer func() { asrTimeout = old }()
	r = recognizerFunc(func(ctx context.Context, _ <-chan []byte, _ func(string)) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if _, e := recognizeWith(context.Background(), make(chan []byte), func(event) {}, config{}, r); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}

func TestFunASRNoFinalWithoutOffline(t *testing.T) {
	old := funasrFinalTimeout
	funasrFinalTimeout = 20 * time.Millisecond
	defer func() { funasrFinalTimeout = old }()
	for _, online := range []string{"", "只有在线"} {
		t.Run(online, func(t *testing.T) {
			url := wsMock(t, func(c *websocket.Conn, _ *http.Request) {
				readStart(t, c)
				receiveStop(t, c, false)
				if online != "" {
					c.WriteJSON(asrMessage{Mode: "online", Text: online})
				}
				for {
					if _, _, e := c.ReadMessage(); e != nil {
						return
					}
				}
			})
			r := &websocketRecognizer{settings: config{ASR: asrConfig{URL: url}}}
			if _, e := r.recognize(context.Background(), testAudio(15), func(string) {}); e == nil || !strings.Contains(e.Error(), "no offline result") {
				t.Fatal(e)
			}
		})
	}
}
func TestOpenAIRequestTimeout(t *testing.T) {
	old := openaiTimeout
	openaiTimeout = 20 * time.Millisecond
	defer func() { openaiTimeout = old }()
	s := memoryServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	r := &openaiRecognizer{asrConfig{URL: s.URL, Model: "mock"}}
	if _, e := r.recognize(context.Background(), testAudio(15), func(string) {}); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestASRShellBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name  string
		audio <-chan []byte
		tail  int
		want  string
	}{
		{"exact-limit", testAudio(3000), 0, ""},
		{"limit-with-tail", testAudio(2990), 200, ""},
		{"tail-over-limit", testAudio(2991), 200, "audio too long"},
		{"bad-frame", func() <-chan []byte { ch := make(chan []byte, 1); ch <- make([]byte, 639); close(ch); return ch }(), 0, "invalid PCM frame"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := recognizerFunc(func(_ context.Context, a <-chan []byte, _ func(string)) (string, error) {
				for range a {
				}
				return "结果", nil
			})
			_, e := recognizeWith(context.Background(), tt.audio, func(event) {}, config{TailMS: tt.tail}, r)
			if tt.want == "" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil || e.Error() != tt.want {
				t.Fatal(e)
			}
		})
	}
}
func TestASRKeyFileBoundaries(t *testing.T) {
	t.Setenv("SIXUAN_ASR_API_KEY", "environment")
	dir := t.TempDir()
	p := filepath.Join(dir, "key")
	for _, tt := range []struct {
		name, content, want string
		valid               bool
	}{
		{"empty", "", "environment", true},
		{"whitespace", " \n\t ", "environment", true},
		{"file-over-inline", " file ", "file", true},
		{"too-large", strings.Repeat("x", 65537), "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if e := os.WriteFile(p, []byte(tt.content), 0600); e != nil {
				t.Fatal(e)
			}
			c := config{ASR: asrConfig{Provider: "funasr", URL: "ws://mock", APIKeyFile: p}}
			if tt.name == "file-over-inline" {
				c.ASR.APIKey = "inline"
			}
			validateASR(&c)
			if (c.configError == "") != tt.valid || tt.valid && c.ASR.APIKey != tt.want {
				t.Fatal("key resolution or validation")
			}
		})
	}
	c := config{ASR: asrConfig{APIKeyFile: dir}}
	validateASR(&c)
	if c.configError == "" {
		t.Fatal("accepted directory")
	}
}

func TestASRShellEmptyAfterPolish(t *testing.T) {
	for _, mode := range []string{"basic", "aggressive"} {
		t.Run(mode, func(t *testing.T) {
			r := recognizerFunc(func(_ context.Context, a <-chan []byte, _ func(string)) (string, error) {
				for range a {
				}
				return "嗯", nil
			})
			_, e := recognizeWith(context.Background(), testAudio(15), func(event) {}, config{ASR: asrConfig{Provider: "openai"}, TextPolish: mode}, r)
			if e == nil || e.Error() != "empty recognition result" {
				t.Fatal(e)
			}
		})
	}
}
