package main

/*
#cgo LDFLAGS: -lm
#include "native.h"
*/
import "C"
import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"github.com/sixuan-ai/sixuan-ime/theme"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type config struct {
	Appearance           theme.Appearance `json:"appearance"`
	appearanceWarning    string
	LocalDictionary      string `json:"local_dictionary"`
	localSchema          string
	localDictionaryError string
	ASR                  asrConfig `json:"asr"`
	Source               string    `json:"source"`
	MaxSeconds           float64   `json:"max_seconds"`
	HoldCtrl             bool      `json:"hold_ctrl"`
	HoldMS               int       `json:"ctrl_hold_ms"`
	Sound                *bool     `json:"sound_feedback"`
	Volume               *float64  `json:"feedback_volume"`
	TailMS               int       `json:"tail_silence_ms"`
	TextPolish           string    `json:"text_polish"`
	PolishTimeoutMS      int       `json:"polish_timeout_ms"`
	configError          string
	PinyinEnabled        bool `json:"pinyin_enabled"`
	CloudCandidates      bool `json:"cloud_candidates"`
	DictionarySync       bool `json:"dictionary_sync"`
	CloudTimeoutMS       int  `json:"cloud_timeout_ms"`
	CloudDebounceMS      int  `json:"cloud_debounce_ms"`
}

func readConfig(overrides ...func(*config)) config {
	h, _ := os.UserHomeDir()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(h, ".config")
	}
	c := config{Appearance: theme.Default(), PinyinEnabled: publicBuild, Source: "@DEFAULT_SOURCE@", MaxSeconds: 55, HoldCtrl: true, HoldMS: 400, TailMS: 500, TextPolish: "off", PolishTimeoutMS: 2500, CloudTimeoutMS: 1200, CloudDebounceMS: 100}
	if publicBuild {
		c.CloudTimeoutMS = 3500
	}
	if b, e := os.ReadFile(filepath.Join(configHome, configDirName+"/config.json")); e == nil {
		candidate := c
		if json.Unmarshal(b, &candidate) != nil {
			c.configError = "配置文件 JSON 格式错误"
		} else {
			c = candidate
		}
	}
	if c.Source == "" {
		c.Source = "@DEFAULT_SOURCE@"
	}
	c.MaxSeconds = math.Max(1, math.Min(55, c.MaxSeconds))
	if c.HoldMS < 300 {
		c.HoldMS = 300
	}
	if c.HoldMS > 2000 {
		c.HoldMS = 2000
	}
	if c.TailMS < 200 {
		c.TailMS = 200
	}
	if c.TailMS > 1000 {
		c.TailMS = 1000
	}
	if c.TextPolish == "" {
		c.TextPolish = "off"
	}
	if c.TextPolish != "off" && c.TextPolish != "basic" && c.TextPolish != "aggressive" {
		c.configError = "text_polish 必须为 off、basic 或 aggressive"
	}
	if c.PolishTimeoutMS < 500 {
		c.PolishTimeoutMS = 500
	}
	if c.PolishTimeoutMS > 15000 {
		c.PolishTimeoutMS = 15000
	}
	if c.CloudTimeoutMS < 200 {
		c.CloudTimeoutMS = 200
	}
	if c.CloudTimeoutMS > 5000 {
		c.CloudTimeoutMS = 5000
	}
	if c.CloudDebounceMS < 50 {
		c.CloudDebounceMS = 50
	}
	if c.CloudDebounceMS > 1000 {
		c.CloudDebounceMS = 1000
	}
	for _, override := range overrides {
		override(&c)
	}
	if publicBuild {
		c.DictionarySync = false
	}
	if c.LocalDictionary == "" {
		c.LocalDictionary = initialDictionaryMode()
	}
	c.localSchema, c.localDictionaryError = resolveLocalDictionary(c.LocalDictionary)
	var appearanceWarnings []string
	c.Appearance, appearanceWarnings = theme.Normalize(c.Appearance)
	c.appearanceWarning = strings.Join(appearanceWarnings, "；")
	validateASR(&c)
	return c
}

type application struct {
	cloudError, dictionaryError  string
	phase, last, delivery, err   string
	generation                   uint64
	started                      time.Time
	firstPartial, stopElapsed    float64
	micReady, speechAt, stopIdle float64
	protocolReady                float64
	sessionWarm                  bool
	uploadRequests               int
	endUpload, endAck            float64
	finalPolls                   int
	cancel                       context.CancelFunc
	stop                         chan struct{}
	timer                        *time.Timer
	gesture                      gesture
	locked, indicator            bool
	config                       config
}

var app *application
var events = make(chan event, 512)
var soundLock sync.Mutex

func post(e event) {
	if e.kind == "level" {
		select {
		case events <- e:
		default:
			return
		}
	} else {
		events <- e
	}
	C.native_post()
}
func cstr(s string) (*C.char, func()) {
	p := C.CString(s)
	return p, func() { C.free(unsafe.Pointer(p)) }
}
func overlay(on bool) {
	if !on {
		C.native_overlay(0, 0, 0, 0)
		app.indicator = false
		return
	}
	p := struct{ X, Y int }{-99999, -99999}
	b, e := os.ReadFile(filepath.Join(stateHome(), stateDirName+"/indicator-position.json"))
	if e == nil {
		json.Unmarshal(b, &p)
	}
	C.native_overlay(1, C.double(time.Since(app.started).Seconds()), C.int(p.X), C.int(p.Y))
	app.indicator = true
}
func cue(kind string) {
	c := app.config
	if c.Sound != nil && !*c.Sound {
		return
	}
	volume := .6
	if c.Volume != nil {
		volume = math.Max(0, math.Min(1, *c.Volume))
	}
	go func() {
		soundLock.Lock()
		defer soundLock.Unlock()
		frequencies := []float64{660, 990}
		if kind == "stop" {
			frequencies = []float64{660, 440}
		} else if kind == "cancel" {
			frequencies = []float64{330}
		}
		data := []byte{}
		for _, frequency := range frequencies {
			n := 1560
			for i := 0; i < n; i++ {
				fade := math.Min(1, math.Min(float64(i)/192, float64(n-1-i)/192))
				v := int16(32767 * .32 * fade * math.Sin(2*math.Pi*frequency*float64(i)/24000))
				data = binary.LittleEndian.AppendUint16(data, uint16(v))
			}
			data = append(data, make([]byte, 720)...)
		}
		playSound(data, volume, kind)
	}()
}
func (a *application) status() string {
	elapsed := 0.0
	if a.phase == "recording" {
		elapsed = math.Round(time.Since(a.started).Seconds()*10) / 10
	}
	speechLatency := 0.0
	if a.firstPartial > 0 && a.speechAt > 0 {
		speechLatency = math.Max(0, a.firstPartial-a.speechAt)
	}
	b, _ := json.Marshal(map[string]any{"appearance": a.config.Appearance, "appearance_warning": a.config.appearanceWarning, "asr_provider": asrProvider(a.config.ASR), "phase": a.phase, "recording_seconds": elapsed, "last_delivery": a.delivery, "has_last_result": a.last != "", "error": a.err, "recording_indicator": a.indicator, "keyboard_devices": int(C.native_keyboards()), "ctrl_hold_ms": a.config.HoldMS, "implementation": "go", "text_polish": a.config.TextPolish, "text_commit_count": int(C.native_voice_commits()), "preedit_update_count": int(C.native_voice_preedits()), "polish_timeout_ms": a.config.PolishTimeoutMS, "pinyin_enabled": a.config.PinyinEnabled, "local_dictionary": a.config.LocalDictionary, "local_dictionary_schema": a.config.localSchema, "local_dictionary_error": a.config.localDictionaryError, "active_pinyin_schema": activePinyinSchema(), "cloud_candidates": a.config.CloudCandidates, "cloud_error": a.cloudError, "dictionary_error": a.dictionaryError, "first_partial_seconds": a.firstPartial, "stop_to_final_seconds": a.stopElapsed, "microphone_ready_seconds": a.micReady, "speech_detected_seconds": a.speechAt, "speech_to_first_partial_seconds": speechLatency, "stop_to_idle_seconds": a.stopIdle, "protocol_ready_seconds": a.protocolReady, "session_reused": a.sessionWarm, "upload_requests": a.uploadRequests, "tail_silence_ms": a.config.TailMS, "end_upload_seconds": a.endUpload, "end_ack_seconds": a.endAck, "final_poll_count": a.finalPolls})
	return string(b)
}
func (a *application) start() {
	if a.phase != "idle" || a.locked {
		return
	}
	a.config = readConfig()
	if a.config.configError != "" {
		a.err = a.config.configError
		notify("配置错误", a.err)
		return
	}
	a.generation++
	gen := a.generation
	a.err = ""
	a.delivery = ""
	a.firstPartial = 0
	a.stopElapsed = 0
	a.micReady = 0
	a.speechAt = 0
	a.stopIdle = 0
	a.protocolReady = 0
	a.sessionWarm = false
	a.uploadRequests = 0
	a.endUpload = 0
	a.endAck = 0
	a.finalPolls = 0
	if C.native_begin(C.uint64_t(gen)) == 0 {
		a.err = "请先点击输入框，或等待输入法恢复"
		return
	}
	a.phase = "recording"
	a.started = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.stop = make(chan struct{})
	stop := a.stop
	audio := make(chan []byte, 3000)
	started := a.started
	emit := func(e event) {
		e.generation = gen
		if e.kind == "ready" || e.kind == "level" {
			e.elapsed = time.Since(started).Seconds()
		}
		post(e)
	}
	go capture(ctx, stop, a.config.Source, audio, emit)
	go func() {
		text, e := recognize(ctx, audio, emit)
		if e != nil {
			if ctx.Err() == nil {
				emit(event{kind: "error", text: asrNotificationMessage(e)})
			}
			return
		}
		emit(event{kind: "final", text: text})
	}()
	C.native_space(1)
	a.timer = time.AfterFunc(time.Duration(a.config.MaxSeconds*float64(time.Second)), func() { post(event{kind: "stop", generation: gen}) })
}

var stoppedAt time.Time

func (a *application) stopRecording() {
	if a.phase != "recording" {
		return
	}
	if a.timer != nil {
		a.timer.Stop()
	}
	a.phase = "stopping"
	stoppedAt = time.Now()
	close(a.stop)
	a.stop = nil
	C.native_space(0)
	overlay(false)
}
func (a *application) cancelRecording(feedback bool) {
	active := a.phase != "idle"
	a.generation++
	if a.timer != nil {
		a.timer.Stop()
	}
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.stop = nil
	C.native_space(0)
	overlay(false)
	C.native_cancel()
	a.phase = "idle"
	if active && feedback {
		cue("cancel")
	}
}
func (a *application) action(s string) {
	switch s {
	case "start":
		a.start()
	case "stop":
		a.stopRecording()
	case "cancel":
		a.cancelRecording(true)
	case "toggle":
		if a.phase == "recording" {
			a.stopRecording()
		} else if a.phase == "idle" {
			a.start()
		}
	}
}

//export goDrain
func goDrain() {
	for {
		select {
		case e := <-events:
			if strings.HasPrefix(e.kind, "pinyin_") || e.kind == "dictionary_status" {
				pinyinEvent(e)
				continue
			}
			if e.kind == "quit" {
				app.cancelRecording(false)
				C.native_quit()
				continue
			}
			if e.kind == "prepare" {
				c := app.gesture.candidate
				if c != nil && !c.chord && !c.fired && c.phase == "idle" && app.phase == "idle" {
					prepareASR()
				}
				continue
			}
			if e.kind == "hold" {
				app.action(app.gesture.check(app.phase))
				continue
			}
			if e.generation != app.generation {
				continue
			}
			switch e.kind {
			case "ready":
				if app.phase == "recording" {
					app.micReady = e.elapsed
					cue("start")
					overlay(true)
					notify("🎙 已开始录音，可以说话了", "点按 Ctrl 或空格结束")
				}
			case "session_ready":
				app.protocolReady = e.elapsed
				app.sessionWarm = e.warm
			case "metrics":
				app.uploadRequests = e.requests
				app.endUpload = e.endUpload
				app.endAck = e.endAck
				app.finalPolls = e.polls
			case "level":
				if app.phase == "recording" {
					if app.speechAt == 0 && e.rms > .008 {
						app.speechAt = e.elapsed
					}
					C.native_level(C.double(e.rms), C.double(e.peak))
				}
			case "partial":
				if app.phase == "recording" || app.phase == "stopping" || app.phase == "transcribing" {
					if app.firstPartial == 0 {
						app.firstPartial = time.Since(app.started).Seconds()
					}
					p, free := cstr(e.text)
					C.native_text(p, 0)
					free()
				}
			case "mic_stopped":
				if app.phase == "stopping" {
					app.phase = "transcribing"
					cue("stop")
				}
			case "stop":
				app.stopRecording()
			case "final":
				if app.phase == "recording" {
					app.cancelRecording(false)
					app.err = "录音意外中断"
					continue
				}
				// A late duplicate completion must not start another insertion.
				if app.phase != "stopping" && app.phase != "transcribing" {
					continue
				}
				app.stopElapsed = time.Since(stoppedAt).Seconds()
				app.last = e.text
				atomicJSON(filepath.Join(stateHome(), stateDirName+"/last-result.json"), map[string]any{"text": e.text, "created_at": time.Now().Unix()})
				app.phase = "inserting"
				p, free := cstr(e.text)
				C.native_text(p, 1)
				free()
			case "error":
				app.cancelRecording(false)
				app.err = e.text
				notify("思玄语音", e.text)
			}
		default:
			return
		}
	}
}

//export goKey
func goKey(device *C.char, code C.int, value C.int) {
	handleKey(C.GoString(device), int(code), int(value))
}
func handleKey(device string, code, value int) {
	if app == nil || app.locked {
		return
	}
	before := app.gesture.candidate
	action := app.gesture.feed(device, code, value, app.phase)
	app.action(action)
	if c := app.gesture.candidate; c != nil && c != before && c.phase == "idle" && !c.fired && !c.chord {
		generation := app.generation
		time.AfterFunc(150*time.Millisecond, func() { post(event{kind: "prepare", generation: generation}) })
		time.AfterFunc(app.gesture.hold, func() { post(event{kind: "hold", generation: generation}) })
	}
}

//export goCommand
func goCommand(method *C.char) *C.char {
	s := C.GoString(method)
	switch s {
	case "PinyinTrace", "PinyinTraceStart", "PinyinTraceStop":
		action := 0
		if s == "PinyinTraceStart" {
			action = 1
		}
		if s == "PinyinTraceStop" {
			action = 2
		}
		p := C.native_pinyin_trace(C.int(action))
		defer C.free(unsafe.Pointer(p))
		return C.CString(C.GoString(p))
	case "Reload":
		reloadPreferences()
	case "Settings":
		C.native_settings_show()
	case "RegisterPinyin":
		if app.config.PinyinEnabled {
			C.native_enable_pinyin(0)
		}
	case "SelectPinyin":
		if app.config.PinyinEnabled {
			C.native_enable_pinyin(1)
		}
	case "Toggle":
		app.action("toggle")
	case "Stop":
		app.action("stop")
	case "Cancel":
		app.action("cancel")
	case "Last":
		b, _ := json.Marshal(map[string]string{"text": app.last})
		return C.CString(string(b))
	case "CopyLast":
		p, free := cstr(app.last)
		C.native_clipboard(p)
		free()
	}
	return C.CString(app.status())
}

//export goDelivered
func goDelivered(gen C.uint64_t, success C.int, reason *C.char) {
	if app == nil || uint64(gen) != app.generation {
		return
	}
	if app.phase != "inserting" {
		if success == 0 {
			message := C.GoString(reason)
			app.cancelRecording(false)
			app.err = message
		}
		return
	}
	app.phase = "idle"
	app.stopIdle = time.Since(stoppedAt).Seconds()
	if success != 0 {
		app.delivery = "ibus"
		app.err = ""
		notify("已输入", "语音文字已提交到输入框")
	} else {
		app.delivery = "saved"
		app.err = C.GoString(reason)
		notify("识别结果已保留", app.err)
	}
	if app.cancel != nil {
		app.cancel()
		app.cancel = nil
	}
}

//export goLocked
func goLocked(locked C.int) {
	if app == nil {
		return
	}
	app.locked = locked != 0
	app.gesture.reset()
	if app.locked && app.phase != "idle" {
		app.cancelRecording(false)
	}
}

//export goPosition
func goPosition(x, y C.int) {
	atomicJSON(filepath.Join(stateHome(), stateDirName+"/indicator-position.json"), map[string]int{"x": int(x), "y": int(y)})
}
func restoreInput() {
	p := filepath.Join(stateHome(), stateDirName+"/input-method.json")
	b, _ := os.ReadFile(p)
	var saved struct{ Engine string }
	json.Unmarshal(b, &saved)
	current := C.native_engine()
	name := C.GoString(current)
	C.free(unsafe.Pointer(current))
	if name != "" && name != voiceEngineID && name != pinyinEngineID {
		return
	}
	if saved.Engine != "" && saved.Engine != voiceEngineID {
		p, free := cstr(saved.Engine)
		defer free()
		for i := 0; i < 5; i++ {
			if C.native_restore(p) != 0 {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}
func daemon() error {
	runtime.LockOSThread()
	restoreInput()
	p := C.native_engine()
	name := C.GoString(p)
	C.free(unsafe.Pointer(p))
	if name != "" && name != voiceEngineID && name != pinyinEngineID {
		atomicJSON(filepath.Join(stateHome(), stateDirName+"/input-method.json"), map[string]string{"engine": name})
	}
	app = &application{phase: "idle", config: readConfig()}
	enableDaemonSessions()
	app.gesture.hold = time.Duration(app.config.HoldMS) * time.Millisecond
	app.gesture.reset()
	b, _ := os.ReadFile(filepath.Join(stateHome(), stateDirName+"/last-result.json"))
	var last struct{ Text string }
	json.Unmarshal(b, &last)
	app.last = last.Text
	keys := 0
	if app.config.HoldCtrl {
		keys = 1
	}
	if C.native_init(C.int(keys)) == 0 {
		return fmt.Errorf("desktop initialization failed")
	}
	C.native_gui_init()
	applyAppearance(app.config.Appearance)
	reportAppearanceWarning(app.config)
	configureDictionary(app.config.DictionarySync)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; post(event{kind: "quit"}) }()
	C.native_run()
	app.cancelRecording(false)
	return nil
}
func control(action string) {
	methods := map[string]string{"pinyin-trace": "PinyinTrace", "pinyin-trace-start": "PinyinTraceStart", "pinyin-trace-stop": "PinyinTraceStop", "toggle": "Toggle", "stop": "Stop", "cancel": "Cancel", "status": "Status", "last": "Last", "copy-last": "CopyLast", "reload": "Reload", "pinyin": "SelectPinyin", "register-pinyin": "RegisterPinyin", "settings": "Settings"}
	method, ok := methods[action]
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown action")
		os.Exit(2)
	}
	p, free := cstr(method)
	defer free()
	r := C.native_control(p)
	defer C.free(unsafe.Pointer(r))
	s := C.GoString(r)
	if action == "last" {
		var v struct{ Text string }
		json.Unmarshal([]byte(s), &v)
		s = v.Text
	}
	fmt.Println(s)
}

// A short asynchronous native playback stream; never opens a microphone.
func playSound(data []byte, volume float64, kind string) {
	for i := 0; i+1 < len(data); i += 2 {
		v := int16(binary.LittleEndian.Uint16(data[i:]))
		binary.LittleEndian.PutUint16(data[i:], uint16(int16(float64(v)*volume)))
	}
	p, free := cstr(kind)
	defer free()
	C.audio_play(unsafe.Pointer(&data[0]), C.size_t(len(data)), p)
}
func notify(title, body string) {
	a, freeA := cstr(title)
	defer freeA()
	b, freeB := cstr(body)
	defer freeB()
	C.native_notify(a, b)
}

func restoreService() { runtime.LockOSThread(); C.native_space(0); restoreInput() }
