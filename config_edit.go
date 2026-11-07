package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func editConfig(values map[string]any) error {
	home, _ := os.UserHomeDir()
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".config")
	}
	p := filepath.Join(dir, configDirName+"/config.json")
	m := map[string]any{}
	if b, e := os.ReadFile(p); e == nil {
		if json.Unmarshal(b, &m) != nil {
			return errors.New("现有配置 JSON 无效，未覆盖")
		}
	}
	for k, v := range values {
		m[k] = v
	}
	return atomicJSON(p, m)
}
