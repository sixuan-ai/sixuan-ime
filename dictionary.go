package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type phrase struct {
	Key    string `json:"key"`
	Text   string `json:"text"`
	Source string `json:"source"`
	Weight int    `json:"weight"`
}

func readPrivateJSON(file string) ([]byte, error) {
	f, e := os.Open(file)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 32<<20+1))
	if e != nil || len(b) > 32<<20 {
		return nil, errors.New("词库文件超过上限")
	}
	return b, nil
}
func wordsPath(name string) string { return filepath.Join(stateHome(), stateDirName, name) }
func readWords(name string) []phrase {
	b, e := readPrivateJSON(wordsPath(name))
	if e != nil {
		return nil
	}
	var rows []phrase
	if json.Unmarshal(b, &rows) != nil {
		return nil
	}
	return rows
}

var phraseCache struct {
	sync.Mutex
	files map[string]cachedPhrases
}

type cachedPhrases struct {
	modified time.Time
	size     int64
	index    map[string][]phrase
}

func phraseCandidates(query string) []phrase {
	q := normalizePinyin(query)
	home, _ := os.UserHomeDir()
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".config")
	}
	files := phraseFiles(dir)
	phraseCache.Lock()
	defer phraseCache.Unlock()
	if phraseCache.files == nil {
		phraseCache.files = map[string]cachedPhrases{}
	}
	rows := []phrase{}
	seen := map[string]bool{}
	for _, file := range files {
		info, e := os.Stat(file)
		if e != nil {
			delete(phraseCache.files, file)
			continue
		}
		cached, ok := phraseCache.files[file]
		if !ok || !info.ModTime().Equal(cached.modified) || info.Size() != cached.size {
			words := []phrase{}
			if filepath.Ext(file) == ".tsv" {
				f, e := os.Open(file)
				if e == nil {
					scanner := bufio.NewScanner(io.LimitReader(f, 4<<20))
					for scanner.Scan() {
						parts := strings.SplitN(scanner.Text(), "\t", 3)
						if len(parts) >= 2 && !strings.HasPrefix(parts[0], "#") {
							words = append(words, phrase{normalizePinyin(parts[0]), parts[1], "自定义", 1000})
						}
					}
					f.Close()
				}
			} else if b, e := readPrivateJSON(file); e == nil {
				json.Unmarshal(b, &words)
			}
			cached = cachedPhrases{modified: info.ModTime(), size: info.Size(), index: map[string][]phrase{}}
			for _, word := range words {
				word.Key = normalizePinyin(word.Key)
				if validPhrase(word.Key, word.Text) {
					cached.index[word.Key] = append(cached.index[word.Key], word)
				}
			}
			phraseCache.files[file] = cached
		}
		for _, word := range cached.index[q] {
			if !seen[word.Text] {
				rows = append(rows, word)
				seen[word.Text] = true
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Weight > rows[j].Weight })
	if len(rows) > 10 {
		rows = rows[:10]
	}
	return rows
}
func learnWord(key, text string) error {
	key = normalizePinyin(key)
	if !validPhrase(key, text) {
		return errors.New("词条格式无效")
	}
	if e := os.MkdirAll(filepath.Dir(wordsPath("learned.lock")), 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(wordsPath("learned.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); e != nil {
		return e
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	rows := readWords("learned-words.json")
	found := false
	for i := range rows {
		if rows[i].Key == key && rows[i].Text == text {
			rows[i].Weight++
			found = true
			break
		}
	}
	if !found {
		rows = append(rows, phrase{key, text, "学习", 1})
	}
	if len(rows) > 50000 {
		return errors.New("本地词条已达到上限")
	}
	return atomicJSON(wordsPath("learned-words.json"), rows)
}
