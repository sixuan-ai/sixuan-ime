package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDictionaryCompilerHelper(t *testing.T) {
	if os.Getenv("SIXUAN_TEST_DICT_HELPER") != "1" {
		return
	}
	if compileLocalDictionary(os.Getenv("SIXUAN_TEST_DICT_STAGE")) != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
func isolateDictionaryTest(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SIXUAN_ASR_API_KEY", "")
	old := reloadRunningService
	reloadRunningService = func() {}
	t.Cleanup(func() { reloadRunningService = old })
}
func TestDictionaryRowsRejectBadInput(t *testing.T) {
	for _, line := range []string{"你好\tNI HAO\t1", "坏\x00字\thuai zi\t1", "字\tzi\t-1", "字\tzi\t4294967296", "字\tzi\tNaN", "字\tzi\t1\textra", "字\t\t1"} {
		if _, _, _, _, err := canonicalDictionaryRow(line); err == nil {
			t.Fatalf("accepted invalid row %q", line)
		}
	}
	got, code, _, _, err := canonicalDictionaryRow("你好\tni'hao\t100")
	if err != nil || code != "ni hao" || got != "你好\tni hao\t100\n" {
		t.Fatal(got, code, err)
	}
}
func TestDictionaryMissingBundleDoesNotBreakVoice(t *testing.T) {
	isolateDictionaryTest(t)
	if err := editConfig(map[string]any{"local_dictionary": "imported", "source": "kept-microphone"}); err != nil {
		t.Fatal(err)
	}
	c := readConfig()
	if c.localSchema != defaultPinyinSchema || c.localDictionaryError == "" || c.Source != "kept-microphone" {
		t.Fatal("invalid import did not fall back safely")
	}
	if err := setPreference("local_dictionary", 1); err == nil {
		t.Fatal("missing bundle enabled")
	}
}
func TestLocalDictionaryImportAndRollback(t *testing.T) {
	isolateDictionaryTest(t)
	old := runDictionaryCompiler
	runDictionaryCompiler = func(stage string) error {
		cmd := exec.Command(os.Args[0], "-test.run=TestDictionaryCompilerHelper")
		cmd.Env = append(os.Environ(), "SIXUAN_TEST_DICT_HELPER=1", "SIXUAN_TEST_DICT_STAGE="+stage)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Log(string(output))
		}
		return err
	}
	t.Cleanup(func() { runDictionaryCompiler = old })
	if err := editConfig(map[string]any{"source": "kept-mic", "text_polish": "aggressive", "custom_field": map[string]any{"nested": true}}); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "dictionary.dict.yaml")
	os.WriteFile(input, []byte("# sample\n---\nname: ignored\n...\n你好\tni hao\t100\n中国\tzhong guo\t90\n"), 0600)
	bundle, err := installLocalDictionary(input)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Rows != 2 || !localSchemaPattern.MatchString(bundle.Schema) {
		t.Fatal(bundle)
	}
	if c := readConfig(); c.LocalDictionary != "default" {
		t.Fatal("import silently enabled dictionary")
	}
	if _, err = loadLocalBundle(true); err != nil {
		t.Fatal(err)
	}
	if err = dictionaryCommand([]string{"use", "imported"}); err != nil {
		t.Fatal(err)
	}
	c := readConfig()
	if c.localSchema != bundle.Schema || c.Source != "kept-mic" || c.TextPolish != "aggressive" {
		t.Fatal("configuration changed unexpectedly")
	}
	raw, err := readConfigDocument()
	if err != nil || !bytes.Contains(raw["custom_field"], []byte("true")) {
		t.Fatal("unknown config lost", err)
	}
	before, _ := os.ReadFile(filepath.Join(dictionaryRoot(), "current.json"))
	os.WriteFile(input, []byte("invalid\tbad\t-1\n"), 0600)
	if _, err = installLocalDictionary(input); err == nil {
		t.Fatal("bad dictionary accepted")
	}
	after, _ := os.ReadFile(filepath.Join(dictionaryRoot(), "current.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("failed import moved current pointer")
	}
	if err = setPreference("local_dictionary", 0); err != nil {
		t.Fatal(err)
	}
	if c := readConfig(); c.localSchema != defaultPinyinSchema {
		t.Fatal("default rollback failed")
	}
	for _, suffix := range []string{".schema.yaml", ".table.bin", ".prism.bin", ".reverse.bin"} {
		target := filepath.Join(dictionaryRuntime(), bundle.Schema+suffix)
		if st, _ := os.Stat(target); st.Mode().Perm() != 0600 {
			t.Fatal("dictionary permissions")
		}
		original, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, []byte("corrupt"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = loadLocalBundle(true); err == nil {
			t.Fatal("corrupt compiled data accepted", suffix)
		}
		if err = dictionaryCommand([]string{"use", "imported"}); err == nil {
			t.Fatal("corrupt data activated", suffix)
		}
		if err = editConfig(map[string]any{"local_dictionary": "imported"}); err != nil {
			t.Fatal(err)
		}
		if c := readConfig(); c.localSchema != defaultPinyinSchema || c.localDictionaryError == "" || c.Source != "kept-mic" {
			t.Fatal("startup did not detect corruption and preserve voice configuration", suffix)
		}
		if err = os.WriteFile(target, original, 0600); err != nil {
			t.Fatal(err)
		}
	}

}
func TestDictionaryCompilerFailurePreservesSelection(t *testing.T) {
	isolateDictionaryTest(t)
	old := runDictionaryCompiler
	runDictionaryCompiler = func(string) error { return os.ErrPermission }
	t.Cleanup(func() { runDictionaryCompiler = old })
	source := filepath.Join(t.TempDir(), "rows.tsv")
	os.WriteFile(source, []byte("你好\tni hao\t1\n"), 0600)
	if _, err := installLocalDictionary(source); err == nil {
		t.Fatal("compiler failure ignored")
	}
	if _, err := os.Stat(filepath.Join(dictionaryRoot(), "current.json")); !os.IsNotExist(err) {
		t.Fatal("failed compile changed pointer")
	}
	if c := readConfig(); c.LocalDictionary != "default" {
		t.Fatal("failed compile changed configuration")
	}
	matches, _ := filepath.Glob(filepath.Join(dictionaryRoot(), ".import-*"))
	if len(matches) != 0 {
		t.Fatal("temporary imports leaked")
	}
}
func TestBundlePathCannotEscapeRoot(t *testing.T) {
	isolateDictionaryTest(t)
	os.MkdirAll(dictionaryRoot(), 0700)
	data, _ := json.Marshal(map[string]string{"schema_id": "../../elsewhere"})
	os.WriteFile(filepath.Join(dictionaryRoot(), "current.json"), data, 0600)
	if _, err := loadLocalBundle(false); err == nil || !strings.Contains(err.Error(), "导入") {
		t.Fatal("invalid schema path accepted")
	}
}

func TestDictionaryRejectsIncompleteAndOversizeSources(t *testing.T) {
	isolateDictionaryTest(t)
	source := filepath.Join(t.TempDir(), "input.tsv")
	for _, content := range []string{"", "# no words\n", "---\nname: sample\n", "你好\tni hao\t1\n坏行\n", "字\tzi\t1\textra\n"} {
		if err := os.WriteFile(source, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := installLocalDictionary(source); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate((128 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err = installLocalDictionary(source); err == nil {
		t.Fatal("oversize source accepted")
	}
}
