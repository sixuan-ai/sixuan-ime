package main

const publicBuild = true
const temporaryPrefix = ".sixuan-"
const configDirName = "sixuan-ime"
const stateDirName = "sixuan-ime"
const voiceEngineID = "sixuan-voice"
const pinyinEngineID = "sixuan-pinyin"

var buildNames = [...]string{
	"sixuan-voice",
	"sixuan-pinyin",
	"sixuan-ime",
	"dev.sixuan.IME",
	"/dev/sixuan/IME",
	"org.freedesktop.IBus.SixuanIME",
	"sixuan-ime",
	"思玄拼音",
	"Rime 本地拼音",
	"思玄输入法设置",
	"思玄输入法设置…",
	"思玄输入法",
	"语音输入 · 拼音",
	"sixuan-ime",
	"思玄麦克风",
	"sixuan-ime-feedback-%s",
	"sixuan-ime-feedback",
	"思玄录音指示器",
	"/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/sixuan-ime-stop/",
	"/usr/bin/sixuan-ime stop",
	"PolyForm-Noncommercial-1.0.0",
	"思玄输入法",
	"sixuan-ime",
	"1.2.4",
	"rime.sixuan-ime",
	"1.2.4",
}
