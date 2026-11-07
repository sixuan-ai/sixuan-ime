package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Keep the original GVariant text and append one tuple; never rebuild or remove sources.
func appendInputSource(value, engine string) (string, error) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "@a(ss)"))
	tuple := `\(\s*('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*")\s*,\s*('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*")\s*\)`
	valid := regexp.MustCompile(`^\[\s*(?:` + tuple + `\s*(?:,\s*` + tuple + `\s*)*,?\s*)?\]$`)
	if !valid.MatchString(value) {
		return "", errors.New("输入源列表格式无效，未修改")
	}
	target := regexp.MustCompile(`\(\s*['"]ibus['"]\s*,\s*['"]` + regexp.QuoteMeta(engine) + `['"]\s*\)`)
	if target.MatchString(value) {
		return value, nil
	}
	body := strings.TrimSpace(value[1 : len(value)-1])
	body = strings.TrimSuffix(body, ",")
	if body != "" {
		body += ", "
	}
	return "[" + body + "('ibus', '" + engine + "')]", nil
}
func setupDesktop() error {
	dir := configDirectory()
	path := filepath.Join(dir, "config.json")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e := os.Chmod(dir, 0700); e != nil {
		return e
	}
	// O_EXCL makes repeated setup safe even when another setup is running.
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e == nil {
		_, e = f.WriteString("{\n  \"pinyin_enabled\": true,\n  \"hold_ctrl\": true,\n  \"text_polish\": \"basic\"\n}\n")
		ce := f.Close()
		if e == nil {
			e = ce
		}
	} else if os.IsExist(e) {
		e = nil
	}
	if e != nil {
		return e
	}
	if _, e = runDesktopCommand("systemctl", "--user", "enable", "--now", "sixuan-ime.service"); e != nil {
		return e
	}
	current, e := runDesktopCommand("gsettings", "get", "org.gnome.desktop.input-sources", "sources")
	if e != nil {
		return e
	}
	updated, e := appendInputSource(current, pinyinEngineID)
	if e != nil {
		return e
	}
	if updated != strings.TrimSpace(current) {
		if _, e = runDesktopCommand("gsettings", "set", "org.gnome.desktop.input-sources", "sources", updated); e != nil {
			return e
		}
	}
	enabled, e := runDesktopCommand("gsettings", "get", "org.gnome.shell", "enabled-extensions")
	if e != nil {
		return e
	}
	enabled = strings.TrimSpace(strings.TrimPrefix(enabled, "@as"))
	uuid := "sixuan-candidates@sixuan.dev"
	if !strings.HasPrefix(enabled, "[") || !strings.HasSuffix(enabled, "]") {
		return errors.New("扩展列表格式无效，未修改")
	}
	if !strings.Contains(enabled, "'"+uuid+"'") && !strings.Contains(enabled, `"`+uuid+`"`) {
		body := strings.TrimSpace(enabled[1 : len(enabled)-1])
		if body != "" {
			body += ", "
		}
		if _, e = runDesktopCommand("gsettings", "set", "org.gnome.shell", "enabled-extensions", "["+body+"'"+uuid+"']"); e != nil {
			return e
		}
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		h, _ := os.UserHomeDir()
		dataHome = filepath.Join(h, ".local/share")
	}
	apps := filepath.Join(dataHome, "applications")
	if e = os.MkdirAll(apps, 0700); e != nil {
		return e
	}
	desktop := "[Desktop Entry]\nType=Application\nName=思玄语音输入\nComment=思玄输入法设置\nExec=/usr/bin/sixuan-ime settings\nIcon=audio-input-microphone\nTerminal=false\nCategories=Settings;Utility;\n"
	if e = os.WriteFile(filepath.Join(apps, "sixuan-ime.desktop"), []byte(desktop), 0644); e != nil {
		return e
	}
	fmt.Println("配置完成，请注销并重新登录，然后按 Super+空格切换到思玄拼音。")
	fmt.Println("配置语音识别：" + officialASRSetupInstructions)
	return nil
}
