package main

/*
#include "native.h"
*/
import "C"

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

const defaultPinyinSchema = "luna_pinyin_simp"

var localSchemaPattern = regexp.MustCompile(`^sixuan_local_[0-9a-f]{16}$`)

type localBundle struct {
	Schema string            `json:"schema_id"`
	SHA256 string            `json:"rows_sha256"`
	Rows   int               `json:"rows"`
	Files  map[string]string `json:"files"`
}
type dictionaryBuildRequest struct{ Schema, ProbeCode, ProbeText string }

func dictionaryRoot() string    { return filepath.Join(stateHome(), stateDirName, "local-dictionaries") }
func dictionaryRuntime() string { return filepath.Join(stateHome(), stateDirName, "rime", "build") }
func readSmallJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(b) > 65536 {
		return errors.New("词库清单无效")
	}
	return json.Unmarshal(b, value)
}
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", errors.New("词库文件必须为普通文件")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func loadLocalBundle(fullVerify bool) (localBundle, error) {
	var pointer struct {
		Schema string `json:"schema_id"`
	}
	var bundle localBundle
	if readSmallJSON(filepath.Join(dictionaryRoot(), "current.json"), &pointer) != nil || !localSchemaPattern.MatchString(pointer.Schema) {
		return bundle, errors.New("请先导入本机词库")
	}
	dir := filepath.Join(dictionaryRoot(), pointer.Schema)
	if readSmallJSON(filepath.Join(dir, "manifest.json"), &bundle) != nil || bundle.Schema != pointer.Schema || bundle.Rows < 1 || bundle.Rows > 1000000 || len(bundle.SHA256) != 64 || bundle.Schema != "sixuan_local_"+bundle.SHA256[:16] {
		return localBundle{}, errors.New("本机词库清单无效")
	}
	for _, suffix := range []string{".schema.yaml", ".table.bin", ".prism.bin", ".reverse.bin"} {
		name := bundle.Schema + suffix
		expected := bundle.Files[name]
		if len(expected) != 64 {
			return localBundle{}, errors.New("本机词库不完整")
		}
		path := filepath.Join(dictionaryRuntime(), name)
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() {
			return localBundle{}, errors.New("本机词库文件缺失")
		}
		if fullVerify {
			actual, err := fileDigest(path)
			if err != nil || actual != expected {
				return localBundle{}, errors.New("本机词库校验失败")
			}
		}
	}
	return bundle, nil
}
func resolveLocalDictionary(mode string) (string, string) {
	if mode == "" || mode == "default" {
		return defaultPinyinSchema, ""
	}
	if mode == "bundled" {
		b, err := ensureBundledDictionary()
		if err != nil {
			return defaultPinyinSchema, err.Error()
		}
		return b.Schema, ""
	}
	if mode != "imported" {
		return defaultPinyinSchema, "local_dictionary 必须为 default、bundled 或 imported"
	}
	bundle, err := loadLocalBundle(true)
	if err != nil {
		return defaultPinyinSchema, err.Error()
	}
	return bundle.Schema, ""
}

//export goPinyinSchema
func goPinyinSchema() *C.char {
	if app != nil {
		return C.CString(app.config.localSchema)
	}
	return C.CString(readConfig().localSchema)
}
func activePinyinSchema() string {
	p := C.native_pinyin_schema_status()
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

func canonicalDictionaryRow(line string) (string, string, string, uint64, error) {
	parts := strings.Split(line, "\t")
	if len(parts) != 3 || !utf8.ValidString(parts[0]) || parts[0] == "" || len([]rune(parts[0])) > 128 {
		return "", "", "", 0, errors.New("词条列无效")
	}
	for _, r := range parts[0] {
		if unicode.IsControl(r) {
			return "", "", "", 0, errors.New("词条包含控制字符")
		}
	}
	code := strings.Join(strings.Fields(strings.ReplaceAll(parts[1], "'", " ")), " ")
	if !validPinyin(strings.ReplaceAll(code, " ", "")) {
		return "", "", "", 0, errors.New("拼音列无效")
	}
	weight, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil {
		return "", "", "", 0, errors.New("权重必须为非负整数")
	}
	return fmt.Sprintf("%s\t%s\t%d\n", parts[0], code, weight), code, parts[0], weight, nil
}

var runDictionaryCompiler = func(stage string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(stage, "compile.log"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(executable, "dictionary", "_compile", stage)
	cmd.Stdout = log
	cmd.Stderr = log
	if cmd.Run() != nil {
		return errors.New("词库编译或加载验证失败；原词库未切换")
	}
	return nil
}

func compileLocalDictionary(stage string) error {
	st, err := os.Lstat(stage)
	if err != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 || !strings.HasPrefix(filepath.Base(stage), ".import-") {
		return errors.New("无效的词库编译暂存目录")
	}
	var request dictionaryBuildRequest
	if readSmallJSON(filepath.Join(stage, "request.json"), &request) != nil || !localSchemaPattern.MatchString(request.Schema) || !validPinyin(request.ProbeCode) || request.ProbeText == "" {
		return errors.New("无效的词库编译请求")
	}
	source, fs := cstr(filepath.Join(stage, "source"))
	defer fs()
	user, fu := cstr(filepath.Join(stage, "compiled"))
	defer fu()
	schema, fi := cstr(request.Schema)
	defer fi()
	code, fc := cstr(request.ProbeCode)
	defer fc()
	text, ft := cstr(request.ProbeText)
	defer ft()
	if C.native_dictionary_build(source, user, schema, code, text) == 0 {
		return errors.New("词库编译失败")
	}
	return nil
}
func dictionarySchema(id string) string {
	return fmt.Sprintf(`schema:
  schema_id: %s
  name: 本机导入拼音
  version: "1"
engine:
  processors: [speller, punctuator, selector, navigator, express_editor]
  segmentors: [abc_segmentor, punct_segmentor, fallback_segmentor]
  translators: [punct_translator, script_translator]
speller:
  alphabet: abcdefghijklmnopqrstuvwxyz
  delimiter: " '"
translator:
  dictionary: %s
  enable_user_dict: true
  enable_sentence: true
menu:
  page_size: 9
punctuator:
  half_shape:
    ',': '，'
    '.': '。'
    '?': '？'
    '!': '！'
    ':': '：'
    ';': '；'
`, id, id)
}
func installLocalDictionary(source string) (localBundle, error) {
	var bundle localBundle
	st, err := os.Stat(source)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 128<<20 {
		return bundle, errors.New("请选择不超过 128 MiB 的词库文件")
	}
	root := dictionaryRoot()
	if err = os.MkdirAll(root, 0700); err != nil {
		return bundle, err
	}
	lock, err := os.OpenFile(filepath.Join(root, "import.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return bundle, err
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return bundle, errors.New("已有词库正在导入")
	}
	stage, err := os.MkdirTemp(root, ".import-")
	if err != nil {
		return bundle, err
	}
	defer os.RemoveAll(stage)
	input, err := os.Open(source)
	if err != nil {
		return bundle, err
	}
	defer input.Close()
	rows, err := os.OpenFile(filepath.Join(stage, "rows.tsv"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return bundle, err
	}
	defer rows.Close()
	hash := sha256.New()
	writer := io.MultiWriter(rows, hash)
	scanner := bufio.NewScanner(io.LimitReader(input, (128<<20)+1))
	scanner.Buffer(make([]byte, 4096), 16384)
	inHeader, sawHeader, seenRows := false, false, false
	lineNo := 0
	probeCode, probeText := "", ""
	var probeWeight uint64
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if !seenRows && (line == "---" || inHeader) {
			if line == "---" && !inHeader {
				inHeader = true
				sawHeader = true
			} else if line == "..." {
				inHeader = false
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seenRows = true
		canonical, code, text, weight, e := canonicalDictionaryRow(line)
		if e != nil {
			return bundle, fmt.Errorf("词库第 %d 行格式无效", lineNo)
		}
		bundle.Rows++
		if bundle.Rows > 1000000 {
			return bundle, errors.New("词库超过 100 万条")
		}
		if probeCode == "" {
			probeCode = code
			probeText = text
			probeWeight = weight
		} else if code == probeCode && weight > probeWeight {
			probeText = text
			probeWeight = weight
		}
		if _, err = io.WriteString(writer, canonical); err != nil {
			return bundle, err
		}
	}
	if err = scanner.Err(); err != nil {
		return bundle, errors.New("词库行过长或读取失败")
	}
	if inHeader || bundle.Rows == 0 || sawHeader && !seenRows {
		return bundle, errors.New("词库为空或 YAML 头未结束")
	}
	bundle.SHA256 = hex.EncodeToString(hash.Sum(nil))
	bundle.Schema = "sixuan_local_" + bundle.SHA256[:16]
	sourceDir := filepath.Join(stage, "source")
	if err = os.Mkdir(sourceDir, 0700); err != nil {
		return bundle, err
	}
	dictionary, err := os.OpenFile(filepath.Join(sourceDir, bundle.Schema+".dict.yaml"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return bundle, err
	}
	_, err = fmt.Fprintf(dictionary, "---\nname: %s\nversion: \"1\"\nsort: by_weight\nuse_preset_vocabulary: false\n...\n", bundle.Schema)
	if err == nil {
		_, err = rows.Seek(0, 0)
	}
	if err == nil {
		_, err = io.Copy(dictionary, rows)
	}
	closeErr := dictionary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return bundle, err
	}
	if err = os.WriteFile(filepath.Join(sourceDir, bundle.Schema+".schema.yaml"), []byte(dictionarySchema(bundle.Schema)), 0600); err != nil {
		return bundle, err
	}
	request := dictionaryBuildRequest{bundle.Schema, strings.ReplaceAll(probeCode, " ", ""), probeText}
	if err = atomicJSON(filepath.Join(stage, "request.json"), request); err != nil {
		return bundle, err
	}
	if err = runDictionaryCompiler(stage); err != nil {
		return bundle, err
	}
	bundle.Files = map[string]string{}
	build := filepath.Join(stage, "compiled", "build")
	for _, suffix := range []string{".schema.yaml", ".table.bin", ".prism.bin", ".reverse.bin"} {
		name := bundle.Schema + suffix
		digest, e := fileDigest(filepath.Join(build, name))
		if e != nil {
			return bundle, errors.New("编译产物不完整")
		}
		bundle.Files[name] = digest
	}
	if err = atomicJSON(filepath.Join(stage, "manifest.json"), bundle); err != nil {
		return bundle, err
	}
	final := filepath.Join(root, bundle.Schema)
	if _, err = os.Stat(final); err == nil {
		// A previous identical import may be reused; never overwrite immutable bundles.
		var previous localBundle
		if readSmallJSON(filepath.Join(final, "manifest.json"), &previous) != nil || previous.SHA256 != bundle.SHA256 || previous.Rows != bundle.Rows {
			return bundle, errors.New("已有词库版本冲突")
		}
		bundle = previous
		build = filepath.Join(final, "compiled", "build")
	} else if !errors.Is(err, os.ErrNotExist) {
		return bundle, err
	} else {
		if err = os.Rename(stage, final); err != nil {
			return bundle, err
		}
		build = filepath.Join(final, "compiled", "build")
	}
	if err = os.MkdirAll(dictionaryRuntime(), 0700); err != nil {
		return bundle, err
	}
	for name, digest := range bundle.Files {
		if !strings.HasPrefix(name, bundle.Schema+".") || filepath.Base(name) != name {
			return bundle, errors.New("无效的词库文件清单")
		}
		data, e := os.ReadFile(filepath.Join(build, name))
		if e != nil {
			return bundle, e
		}
		actual := sha256.Sum256(data)
		if hex.EncodeToString(actual[:]) != digest {
			return bundle, errors.New("词库产物校验失败")
		}
		target := filepath.Join(dictionaryRuntime(), name)
		if st, e := os.Lstat(target); e == nil {
			if !st.Mode().IsRegular() {
				return bundle, errors.New("词库目标不是普通文件")
			}
			got, e := fileDigest(target)
			if e != nil || got != digest {
				return bundle, errors.New("已有词库产物冲突")
			}
			continue
		} else if !errors.Is(e, os.ErrNotExist) {
			return bundle, e
		}
		if err = writePrivateFile(target, data); err != nil {
			return bundle, err
		}
	}
	if err = atomicJSON(filepath.Join(root, "current.json"), map[string]string{"schema_id": bundle.Schema}); err != nil {
		return bundle, err
	}
	reloadRunningService()
	return bundle, nil
}
func dictionaryCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("用法：dictionary status|import 文件|use default|bundled|imported")
	}
	switch args[0] {
	case "_compile":
		if len(args) != 2 {
			return errors.New("缺少暂存目录")
		}
		return compileLocalDictionary(args[1])
	case "status":
		c := readConfig()
		bundle, err := loadLocalBundle(false)
		customAvailable := err == nil
		builtin, builtinErr := loadBundledDictionary()
		if c.LocalDictionary == "bundled" {
			bundle = builtin
			err = builtinErr
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"selected": c.LocalDictionary, "schema": c.localSchema, "error": c.localDictionaryError, "available": err == nil, "bundle": bundle, "custom_available": customAvailable, "bundled_available": builtinErr == nil, "bundled_rows": builtin.Rows})
	case "import":
		if len(args) != 2 {
			return errors.New("用法：dictionary import 词库.tsv或.dict.yaml")
		}
		bundle, err := installLocalDictionary(args[1])
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(bundle)
	case "use":
		if len(args) != 2 || (args[1] != "default" && args[1] != "imported" && args[1] != "bundled") {
			return errors.New("请选择 default、bundled 或 imported")
		}
		if args[1] == "bundled" {
			if _, err := ensureBundledDictionary(); err != nil {
				return err
			}
		}
		if args[1] == "imported" {
			if _, err := loadLocalBundle(true); err != nil {
				return err
			}
		}
		if err := editConfig(map[string]any{"local_dictionary": args[1]}); err != nil {
			return err
		}
		reloadRunningService()
		return nil
	}
	return errors.New("未知词库命令")
}
