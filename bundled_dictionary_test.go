package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testDictionaryCompiler(t *testing.T) {
	t.Helper()
	old := runDictionaryCompiler
	runDictionaryCompiler = func(stage string) error {
		c := exec.Command(os.Args[0], "-test.run=TestDictionaryCompilerHelper")
		c.Env = append(os.Environ(), "SIXUAN_TEST_DICT_HELPER=1", "SIXUAN_TEST_DICT_STAGE="+stage)
		return c.Run()
	}
	t.Cleanup(func() { runDictionaryCompiler = old })
}
func fixtureBundledDictionary(t *testing.T) (localBundle, localBundle) {
	t.Helper()
	isolateDictionaryTest(t)
	testDictionaryCompiler(t)
	source := filepath.Join(t.TempDir(), "words.tsv")
	if err := os.WriteFile(source, []byte("你好\tni hao\t100\n中国\tzhong guo\t90\n"), 0600); err != nil {
		t.Fatal(err)
	}
	custom, err := installLocalDictionary(source)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	old := bundledDictionaryDirectory
	bundledDictionaryDirectory = func() string { return root }
	t.Cleanup(func() { bundledDictionaryDirectory = old })
	bundled := localBundle{Schema: strings.Replace(custom.Schema, "sixuan_local_", "sixuan_builtin_", 1), SHA256: custom.SHA256, Rows: custom.Rows, Files: map[string]string{}}
	for _, suffix := range dictionarySuffixes {
		data, err := os.ReadFile(filepath.Join(dictionaryRuntime(), custom.Schema+suffix))
		if err != nil {
			t.Fatal(err)
		}
		if suffix == ".schema.yaml" {
			data = bytes.ReplaceAll(data, []byte(custom.Schema), []byte(bundled.Schema))
		}
		name := bundled.Schema + suffix
		bundled.Files[name] = dataSHA256(data)
		if err = os.WriteFile(filepath.Join(root, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err = atomicJSON(filepath.Join(root, "manifest.json"), bundled); err != nil {
		t.Fatal(err)
	}
	return bundled, custom
}
func TestBundledDefaultSelectionAndExplicitChoices(t *testing.T) {
	b, custom := fixtureBundledDictionary(t)
	pointer, _ := os.ReadFile(filepath.Join(dictionaryRoot(), "current.json"))
	c := readConfig()
	if publicBuild && (c.LocalDictionary != "bundled" || c.localSchema != b.Schema || c.localDictionaryError != "") {
		t.Fatal("fresh public installation did not select bundle")
	}
	if !publicBuild && c.LocalDictionary != "default" {
		t.Fatal("private installation implicitly changed")
	}
	if err := editConfig(map[string]any{"source": "keep-microphone", "text_polish": "aggressive", "custom_field": true}); err != nil {
		t.Fatal(err)
	}
	for mode, schema := range map[string]string{"bundled": b.Schema, "default": defaultPinyinSchema, "imported": custom.Schema} {
		if err := dictionaryCommand([]string{"use", mode}); err != nil {
			t.Fatal(mode, err)
		}
		c = readConfig()
		if c.localSchema != schema || c.Source != "keep-microphone" || c.TextPolish != "aggressive" {
			t.Fatal("choice or configuration lost", mode)
		}
	}
	if err := dictionaryCommand([]string{"use", "bundled"}); err != nil {
		t.Fatal(err)
	}
	if preferenceValue(readConfig(), "local_dictionary") != 1 {
		t.Fatal("bundled toggle is not enabled")
	}
	if err := setPreference("local_dictionary", 0); err != nil {
		t.Fatal(err)
	}
	if readConfig().localSchema != defaultPinyinSchema {
		t.Fatal("toggle rollback failed")
	}
	after, _ := os.ReadFile(filepath.Join(dictionaryRoot(), "current.json"))
	if !bytes.Equal(pointer, after) {
		t.Fatal("bundle overwrote custom selection")
	}
}
func TestBundledCacheRepairAndCorruptSourceFallback(t *testing.T) {
	b, _ := fixtureBundledDictionary(t)
	if err := dictionaryCommand([]string{"use", "bundled"}); err != nil {
		t.Fatal(err)
	}
	name := b.Schema + ".table.bin"
	cache := filepath.Join(dictionaryRuntime(), name)
	if err := os.WriteFile(cache, []byte("broken cache"), 0600); err != nil {
		t.Fatal(err)
	}
	c := readConfig()
	if c.localSchema != b.Schema || c.localDictionaryError != "" {
		t.Fatal("valid bundle did not repair cache")
	}
	digest, _ := fileDigest(cache)
	if digest != b.Files[name] {
		t.Fatal("cache repair bytes incorrect")
	}
	if err := os.WriteFile(filepath.Join(bundledDictionaryDirectory(), name), []byte("broken installed bundle"), 0644); err != nil {
		t.Fatal(err)
	}
	c = readConfig()
	if c.localSchema != defaultPinyinSchema || c.localDictionaryError == "" {
		t.Fatal("corrupt installed data did not fall back")
	}
	if err := dictionaryCommand([]string{"use", "bundled"}); err == nil {
		t.Fatal("corrupt source activated")
	}
}
func TestBundledToggleWithoutCustomImport(t *testing.T) {
	b, _ := fixtureBundledDictionary(t)
	if err := os.Remove(filepath.Join(dictionaryRoot(), "current.json")); err != nil {
		t.Fatal(err)
	}
	if err := setPreference("local_dictionary", 1); err != nil {
		t.Fatal(err)
	}
	if c := readConfig(); c.LocalDictionary != "bundled" || c.localSchema != b.Schema {
		t.Fatal("toggle requires manual import despite installed bundle")
	}
}
