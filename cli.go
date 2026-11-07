package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const releaseVersion = "1.2.5"

func configDirectory() string {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		h, _ := os.UserHomeDir()
		root = filepath.Join(h, ".config")
	}
	return filepath.Join(root, configDirName)
}
func readConfigDocument() (map[string]json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	b, e := os.ReadFile(filepath.Join(configDirectory(), "config.json"))
	if errors.Is(e, os.ErrNotExist) {
		return m, nil
	}
	if e != nil {
		return nil, e
	}
	if json.Unmarshal(b, &m) != nil || m == nil {
		return nil, errors.New("现有配置 JSON 无效，未覆盖")
	}
	return m, nil
}

// Replace through a same-directory temporary file. Never follow a destination symlink.
func writePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e := os.Chmod(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".sixuan-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}

// Only the asr member is replaced; unknown top-level members survive unchanged.
func configureASR(args []string, input io.Reader) error {
	fs := flag.NewFlagSet("asr config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	provider := fs.String("provider", "", "provider")
	url := fs.String("url", "", "url")
	model := fs.String("model", "", "model")
	keyStdin := fs.Bool("api-key-stdin", false, "read key from stdin")
	var hotwords stringList
	fs.Var(&hotwords, "hotword", "hotword (repeatable)")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return errors.New("asr config 不接受位置参数")
	}
	doc, e := readConfigDocument()
	if e != nil {
		return e
	}
	var asr asrConfig
	if b, ok := doc["asr"]; ok && json.Unmarshal(b, &asr) != nil {
		return errors.New("现有 asr 配置无效")
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "provider":
			asr.Provider = *provider
		case "url":
			asr.URL = *url
		case "model":
			asr.Model = *model
		case "hotword":
			asr.Hotwords = hotwords
		}
	})
	// Validate fields before writing anything; key resolution is separate from configuration.
	check := config{ASR: asr}
	check.ASR.APIKeyFile = ""
	validateASR(&check)
	if check.configError != "" {
		return errors.New(check.configError)
	}
	asr.URL = check.ASR.URL
	var key []byte
	if *keyStdin {
		key, e = io.ReadAll(io.LimitReader(input, 65537))
		if e != nil {
			return errors.New("无法读取标准输入密钥")
		}
		key = []byte(strings.TrimSpace(string(key)))
		if len(key) == 0 || len(key) > 65536 {
			return errors.New("密钥不能为空且不能超过 65536 字节")
		}
		asr.APIKey = ""
		asr.APIKeyFile = filepath.Join(configDirectory(), "asr-key")
	}
	b, e := json.Marshal(asr)
	if e != nil {
		return e
	}
	doc["asr"] = b
	data, e := json.MarshalIndent(doc, "", "  ")
	if e != nil {
		return e
	}
	if *keyStdin {
		if e = writePrivateFile(asr.APIKeyFile, key); e != nil {
			return e
		}
	}
	if e = writePrivateFile(filepath.Join(configDirectory(), "config.json"), append(data, '\n')); e != nil {
		return e
	}
	reloadRunningService()
	return nil
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func asrStatus(out io.Writer) error {
	doc, e := readConfigDocument()
	if e != nil {
		return e
	}
	var c asrConfig
	if b, ok := doc["asr"]; ok && json.Unmarshal(b, &c) != nil {
		return errors.New("asr 配置无效")
	}
	hasKey := c.APIKey != "" || strings.TrimSpace(os.Getenv("SIXUAN_ASR_API_KEY")) != ""
	if c.APIKeyFile != "" {
		st, e := os.Stat(c.APIKeyFile)
		hasKey = hasKey || e == nil && st.Mode().IsRegular() && st.Size() > 0
	}
	return json.NewEncoder(out).Encode(map[string]any{"provider": asrProvider(c), "url": effectiveASRURL(c), "has_api_key": hasKey})
}

var runDesktopCommand = func(name string, args ...string) (string, error) {
	b, e := exec.Command(name, args...).CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("%s 失败", name)
	}
	return strings.TrimSpace(string(b)), nil
}

func commonCLI(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "--version":
		kind := "private"
		if publicBuild {
			kind = "public"
		}
		fmt.Printf("%s %s (%s)\n", configDirName, releaseVersion, kind)
		return true, nil
	case "dictionary":
		return true, dictionaryCommand(args[1:])
	case "cloud":
		return true, cloudCommand(args[1:])
	case "setup":
		return true, setupDesktop()
	case "asr":
		if len(args) > 1 {
			switch args[1] {
			case "config":
				return true, configureASR(args[2:], os.Stdin)
			case "status":
				return true, asrStatus(os.Stdout)
			}
		}
	}
	return false, nil
}
