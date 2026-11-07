package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDictionaryConcurrentLearningAndCache(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := learnWord("ni hao", "你好"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	rows := phraseCandidates("nihao")
	if len(rows) != 1 || rows[0].Weight != 20 {
		t.Fatal(rows)
	}
	if err := learnWord("nihao", "你好呀"); err != nil {
		t.Fatal(err)
	}
	if len(phraseCandidates("nihao")) != 2 {
		t.Fatal("cache not invalidated")
	}
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), configDirName)
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "phrases.tsv"), []byte("sxkj\t思玄科技\n"), 0600)
	if r := phraseCandidates("sxkj"); len(r) != 1 || r[0].Text != "思玄科技" {
		t.Fatal(r)
	}
	if st, _ := os.Stat(wordsPath("learned-words.json")); st.Mode().Perm() != 0600 {
		t.Fatal("private mode")
	}
}
