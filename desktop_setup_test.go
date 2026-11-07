package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsGNOMEDesktop(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"ubuntu:GNOME", true}, {"GNOME", true}, {"gnome", true}, {"KDE", false}, {"", false}, {"XGNOME", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("XDG_CURRENT_DESKTOP", tc.value)
			if got := isGNOMEDesktop(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAppendPreloadEngine(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		invalid     bool
	}{
		{"[]", "['target']", false},
		{"@as []", "['target']", false},
		{"['a', 'b']", "['a', 'b', 'target']", false},
		{"['target', 'a']", "['target', 'a']", false},
		{`["target", 'a']`, `["target", 'a']`, false},
		{"['a',]", "['a', 'target']", false},
		{"garbage", "", true}, {"[1]", "", true}, {"['a' 'b']", "", true},
		{"[('ibus', 'a')]", "", true}, {"['unclosed]", "", true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := appendPreloadEngine(tc.input, "target")
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestSetupKDEMocked(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		failGet, failSet  bool
	}{
		{name: "empty", input: "[]", want: "['" + pinyinEngineID + "']"},
		{name: "typed-empty", input: "@as []", want: "['" + pinyinEngineID + "']"},
		{name: "typed-existing", input: "@as ['" + pinyinEngineID + "']", want: "['" + pinyinEngineID + "']"},
		{name: "existing", input: "['" + pinyinEngineID + "', 'other']", want: "['" + pinyinEngineID + "', 'other']"},
		{name: "append", input: "['first', 'second']", want: "['first', 'second', '" + pinyinEngineID + "']"},
		{name: "get-failure", failGet: true},
		{name: "set-failure", input: "[]", failSet: true},
		{name: "invalid", input: "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			output, err := os.CreateTemp(root, "output")
			if err != nil {
				t.Fatal(err)
			}
			oldOutput, oldCommand := os.Stdout, runDesktopCommand
			os.Stdout = output
			t.Cleanup(func() { os.Stdout = oldOutput; runDesktopCommand = oldCommand; output.Close() })
			serviceCalls, getCalls, setCalls := 0, 0, 0
			runDesktopCommand = func(name string, args ...string) (string, error) {
				for _, arg := range args {
					if strings.Contains(arg, "org.gnome.") {
						t.Fatalf("GNOME command: %v", args)
					}
				}
				if name == "systemctl" {
					if strings.Join(args, " ") != "--user enable --now sixuan-ime.service" {
						t.Fatal(args)
					}
					serviceCalls++
					return "", nil
				}
				if name != "gsettings" || len(args) < 3 || args[1] != "org.freedesktop.ibus.general" || args[2] != "preload-engines" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				switch args[0] {
				case "get":
					getCalls++
					if tc.failGet {
						return "", errors.New("schema unavailable")
					}
					return tc.input, nil
				case "set":
					setCalls++
					if tc.failSet {
						return "", errors.New("write failed")
					}
					if len(args) != 4 || args[3] != tc.want {
						t.Fatalf("unexpected write: %v", args)
					}
				default:
					t.Fatal(args)
				}
				return "", nil
			}
			if err := setupDesktop(); err != nil {
				t.Fatal(err)
			}
			wantSets := 0
			if tc.failSet || (tc.want != "" && tc.want != strings.TrimSpace(strings.TrimPrefix(tc.input, "@as"))) {
				wantSets = 1
			}
			if serviceCalls != 1 || getCalls != 1 || setCalls != wantSets {
				t.Fatalf("calls: service=%d get=%d set=%d", serviceCalls, getCalls, setCalls)
			}
			for _, path := range []string{filepath.Join(configDirectory(), "config.json"), filepath.Join(root, "data/applications/sixuan-ime.desktop")} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
			}
			printed, err := os.ReadFile(output.Name())
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{"kimpanel-ibus-panel", "GTK_IM_MODULE=ibus QT_IM_MODULE=ibus XMODIFIERS=@im=ibus", "输入法面板", "虚拟键盘选择「无」", "ibus engine " + pinyinEngineID, "配置语音识别：" + officialASRSetupInstructions} {
				if !strings.Contains(string(printed), required) {
					t.Fatalf("missing %q in output", required)
				}
			}
			if (tc.failGet || tc.failSet || tc.input == "invalid") && !strings.Contains(string(printed), "提示：") {
				t.Fatal("missing nonfatal failure guidance")
			}
		})
	}
}
