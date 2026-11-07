package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type recognizer interface {
	// Consume PCM frames until audio closes; partial replaces the complete hypothesis.
	recognize(context.Context, <-chan []byte, func(string)) (string, error)
}
type asrConfig struct {
	Provider   string   `json:"provider"`
	URL        string   `json:"url"`
	APIKey     string   `json:"api_key"`
	APIKeyFile string   `json:"api_key_file"`
	Model      string   `json:"model"`
	Hotwords   []string `json:"hotwords"`
}

const officialASRURL = "wss://sixuanai.com/v1/asr/stream"
const officialASRSetupInstructions = "登录 sixuanai.com 账号页创建 API Key，然后运行 sixuan-ime asr config --provider sixuan --api-key-stdin"

func effectiveASRURL(c asrConfig) string {
	if c.Provider == "sixuan" && strings.TrimSpace(c.URL) == "" {
		return officialASRURL
	}
	return c.URL
}

var asrTimeout = 120 * time.Second
var sixuanReadyTimeout = 5 * time.Second
var sixuanFinalTimeout = 15 * time.Second
var funasrFinalTimeout = 10 * time.Second
var funasrIdleTimeout = 1500 * time.Millisecond
var openaiTimeout = 30 * time.Second
var asrWriteTimeout = 10 * time.Second

func validateASRKey(c *config) {
	if c.ASR.APIKeyFile != "" {
		f, e := os.Open(c.ASR.APIKeyFile)
		if e != nil {
			c.configError = "无法读取 asr.api_key_file"
			return
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() & ^os.FileMode(0600) != 0 {
			c.configError = "asr.api_key_file 必须为普通文件且权限不得宽于 0600"
			return
		}
		// Read only the explicitly configured key file; never include its contents in errors.
		b, e := io.ReadAll(io.LimitReader(f, 65537))
		if e != nil {
			c.configError = "无法读取 asr.api_key_file"
			return
		}
		if len(b) > 65536 {
			c.configError = "asr.api_key_file 过大"
			return
		}
		c.ASR.APIKey = strings.TrimSpace(string(b))
	}
	if c.ASR.APIKey == "" {
		c.ASR.APIKey = strings.TrimSpace(os.Getenv("SIXUAN_ASR_API_KEY"))
	}
}

type asrConfigContextKey struct{}

func recognize(ctx context.Context, audio <-chan []byte, emit func(event)) (string, error) {
	settings, ok := ctx.Value(asrConfigContextKey{}).(config)
	if !ok {
		settings = readConfig()
	}
	if settings.configError != "" {
		return "", errors.New(settings.configError)
	}
	if handled, text, err := recognizeBuild(ctx, audio, emit, settings); handled {
		return text, err
	}
	var r recognizer
	switch settings.ASR.Provider {
	case "sixuan":
		r = &websocketRecognizer{settings: settings, sixuan: true}
	case "funasr":
		r = &websocketRecognizer{settings: settings}
	case "openai":
		r = &openaiRecognizer{settings.ASR}
	default:
		return "", fmt.Errorf("未知 asr.provider")
	}
	return recognizeWith(ctx, audio, emit, settings, r)
}

func recognizeWith(ctx context.Context, audio <-chan []byte, emit func(event), c config, r recognizer) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, asrTimeout)
	defer cancel()
	providerCtx, providerCancel := context.WithCancel(ctx)
	defer providerCancel()
	prepared := make(chan []byte)
	validation := make(chan error, 1)
	go func() {
		defer close(prepared)
		fail := func(e error) { validation <- e; providerCancel() }
		send := func(b []byte) bool {
			select {
			case prepared <- b:
				return true
			case <-providerCtx.Done():
				return false
			}
		}
		total := 0
		for {
			select {
			case <-providerCtx.Done():
				validation <- providerCtx.Err()
				return
			case b, ok := <-audio:
				if !ok {
					if total < 9600 {
						fail(errors.New("recording too short"))
						return
					}
					// Match legacy's whole 20 ms tail frames; tail counts toward the 60 s wire limit.
					for i := 0; i < c.TailMS/20; i++ {
						if total+640 > 16000*2*60 {
							fail(errors.New("audio too long"))
							return
						}
						total += 640
						if !send(make([]byte, 640)) {
							validation <- providerCtx.Err()
							return
						}
					}
					validation <- nil
					return
				}
				if len(b) != 640 {
					fail(errors.New("invalid PCM frame"))
					return
				}
				total += len(b)
				if total > 16000*2*60 {
					fail(errors.New("audio too long"))
					return
				}
				if !send(b) {
					validation <- providerCtx.Err()
					return
				}
			}
		}
	}()
	last := ""
	text, err := r.recognize(providerCtx, prepared, func(s string) {
		if s != last {
			last = s
			emit(event{kind: "partial", text: s})
		}
	})
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	select {
	case e := <-validation:
		if e != nil {
			return "", e
		}
	default:
		if err == nil {
			return "", errors.New("recognizer ended before audio")
		}
	}
	if err != nil {
		return "", err
	}
	if (publicBuild || c.ASR.Provider != "sixuan") && c.TextPolish != "off" {
		text = localPolish(text, c.TextPolish)
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("empty recognition result")
	}
	return text, nil
}
