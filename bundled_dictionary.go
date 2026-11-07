package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

var bundledSchemaPattern = regexp.MustCompile(`^sixuan_builtin_[0-9a-f]{16}$`)
var dictionarySuffixes = []string{".schema.yaml", ".table.bin", ".prism.bin", ".reverse.bin"}

// The relative path also supports inspecting an unpacked .deb without installing it.
var bundledDictionaryDirectory = func() string {
	executable, err := os.Executable()
	if err == nil && filepath.Base(filepath.Dir(executable)) == "bin" {
		return filepath.Join(filepath.Dir(filepath.Dir(executable)), "share", "sixuan-ime", "dictionary")
	}
	return "/usr/share/sixuan-ime/dictionary"
}

func initialDictionaryMode() string {
	if publicBuild {
		if _, err := os.Stat(filepath.Join(bundledDictionaryDirectory(), "manifest.json")); err == nil {
			return "bundled"
		}
	}
	return "default"
}
func loadBundledDictionary() (localBundle, error) {
	var b localBundle
	root := bundledDictionaryDirectory()
	if readSmallJSON(filepath.Join(root, "manifest.json"), &b) != nil || !bundledSchemaPattern.MatchString(b.Schema) || b.Rows < 1 || b.Rows > 1000000 || len(b.SHA256) != 64 || b.Schema != "sixuan_builtin_"+b.SHA256[:16] {
		return b, errors.New("内置词库清单无效或未安装")
	}
	for _, suffix := range dictionarySuffixes {
		name := b.Schema + suffix
		st, err := os.Lstat(filepath.Join(root, name))
		if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<20 {
			return b, errors.New("内置词库文件缺失或无效")
		}
		digest, err := fileDigest(filepath.Join(root, name))
		if err != nil || len(b.Files[name]) != 64 || digest != b.Files[name] {
			return b, errors.New("内置词库校验失败")
		}
	}
	return b, nil
}
func ensureBundledDictionary() (localBundle, error) {
	b, err := loadBundledDictionary()
	if err != nil {
		return b, err
	}
	if err = os.MkdirAll(dictionaryRuntime(), 0700); err != nil {
		return b, err
	}
	for _, suffix := range dictionarySuffixes {
		name := b.Schema + suffix
		dst := filepath.Join(dictionaryRuntime(), name)
		if st, e := os.Lstat(dst); e == nil {
			if !st.Mode().IsRegular() {
				return b, errors.New("内置词库缓存不是普通文件")
			}
			if digest, e := fileDigest(dst); e == nil && digest == b.Files[name] {
				continue
			}
		} else if !os.IsNotExist(e) {
			return b, e
		}
		data, e := os.ReadFile(filepath.Join(bundledDictionaryDirectory(), name))
		if e != nil {
			return b, e
		}
		// Verify the exact bytes again; source files can change during package upgrades.
		if dataSHA256(data) != b.Files[name] {
			return b, errors.New("内置词库更新中，请稍后重试")
		}
		if e = writePrivateFile(dst, data); e != nil {
			return b, e
		}
	}
	return b, nil
}

func dataSHA256(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
