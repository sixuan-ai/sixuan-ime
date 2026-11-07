package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestDesignVectors(t *testing.T) {
	b, e := os.ReadFile("testdata/presets.json")
	if e != nil {
		t.Fatal(e)
	}
	var vectors []struct {
		ID string `json:"id"`
		Tokens
	}
	if e = json.Unmarshal(b, &vectors); e != nil {
		t.Fatal(e)
	}
	if len(vectors) != 10 {
		t.Fatal(len(vectors))
	}
	for _, v := range vectors {
		t.Run(v.ID, func(t *testing.T) {
			p, ok := Find(v.ID)
			if !ok {
				t.Fatal(v.ID)
			}
			a := Default()
			a.Preset = p.ID
			a.Accent = p.Accent
			a.Base = p.Base
			got := Derive(a)
			got.Contrast = 0
			// JS Number and Go float64 have the same arithmetic, including the contrast ratio.
			if !reflect.DeepEqual(got, v.Tokens) {
				t.Errorf("got %#v, want %#v", got, v.Tokens)
				for k, c := range v.Colors {
					if got.Colors[k] != c {
						t.Errorf("%s: %s != %s", k, got.Colors[k], c)
					}
				}
			}
		})
	}
}
func TestAppearanceParsing(t *testing.T) {
	tests := []string{`{}`, `null`, `"invalid"`, `[]`, `{"accent":"red","base":"#12345G","radius":"no","glow":"no","candidate_size":"no","opacity":0}`, `{"accent":4,"base":false,"opacity":"x"}`}
	for _, input := range tests {
		var a Appearance
		if e := json.Unmarshal([]byte(input), &a); e != nil {
			t.Fatal(e)
		}
		a, w := Normalize(a)
		if !reflect.DeepEqual(Derive(a), Derive(Default())) {
			t.Fatalf("%s: %#v", input, a)
		}
		if input != "{}" && len(w) == 0 {
			t.Fatal("missing warning", input)
		}
	}
	var a Appearance
	json.Unmarshal([]byte(`{"preset":"cyber"}`), &a)
	if a.Accent != "#00F0FF" || Derive(a).Colors["accent-2"] != "#FF3EC8" {
		t.Fatal(a)
	}
	json.Unmarshal([]byte(`{"preset":"custom","accent":"#abcdef","base":"#123456","radius":"square","glow":"strong","candidate_size":"large","opacity":0.7}`), &a)
	if a.Accent != "#ABCDEF" || Derive(a).Glow != 1.7 || Derive(a).Size != 19 || Derive(a).Radius != [3]int{6, 4, 8} {
		t.Fatal(a)
	}
}
func TestCSS(t *testing.T) {
	for _, p := range Presets {
		a := Default()
		a.Preset = p.ID
		a.Accent = p.Accent
		a.Base = p.Base
		for _, surface := range []string{"settings", "overlay", "candidates"} {
			css := CSS(a, surface)
			if strings.Contains(css, "{{") {
				t.Errorf("unresolved %s: %s", surface, css)
			}
			if !strings.Contains(css, p.Accent) {
				t.Errorf("missing accent %s %s", p.ID, surface)
			}
		}
		for _, block := range strings.Split(CSS(a, "candidates"), "}") {
			if !strings.Contains(block, "{") {
				continue
			}
			selector := strings.Split(block, "{")[0]
			if i := strings.LastIndex(selector, "*/"); i >= 0 {
				selector = selector[i+2:]
			}
			for _, s := range strings.Split(selector, ",") {
				if !strings.HasPrefix(strings.TrimSpace(s), "."+CandidateScope) {
					t.Fatal(s)
				}
			}
		}
	}
	a := Default()
	a.Glow = "off"
	a.Radius = "square"
	a.CandidateSize = "large"
	a.Opacity = .7
	css := CSS(a, "candidates")
	for _, want := range []string{"font-size: 19px", "border-radius: 6px", "border-radius: 4px", "rgba(22,27,30,0.7)", "rgba(61,255,140,0)"} {
		if !strings.Contains(css, want) {
			t.Error(want)
		}
	}
}
func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "theme", "candidates.css")
	first := []byte(strings.Repeat("a", 4096))
	second := []byte(strings.Repeat("b", 8192))
	if e := AtomicWrite(path, first); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	stop := make(chan struct{})
	bad := make(chan string, 1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, e := os.ReadFile(path)
			if e != nil || (!reflect.DeepEqual(b, first) && !reflect.DeepEqual(b, second)) {
				select {
				case bad <- "partial read":
				default:
				}
				return
			}
		}
	}()
	for i := 0; i < 40; i++ {
		data := first
		if i%2 == 0 {
			data = second
		}
		if e := AtomicWrite(path, data); e != nil {
			t.Fatal(e)
		}
	}
	close(stop)
	wg.Wait()
	select {
	case e := <-bad:
		t.Fatal(e)
	default:
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal(entries)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if e := AtomicWrite(filepath.Join(path, "bad"), first); e == nil {
		t.Fatal("expected failure")
	}
	b, _ := os.ReadFile(path)
	if !reflect.DeepEqual(b, first) {
		t.Fatal("previous file lost")
	}
}

func TestDefaultRendererCompatibility(t *testing.T) {
	b, e := os.ReadFile("testdata/neon-render.json")
	if e != nil {
		t.Fatal(e)
	}
	var expected map[string]string
	if e = json.Unmarshal(b, &expected); e != nil {
		t.Fatal(e)
	}
	canonical := func(s string) string {
		// CSS whitespace and leading zeroes do not affect raster output.
		s = strings.Join(strings.Fields(s), "")
		s = strings.ReplaceAll(s, ",.", ",0.")
		return s
	}
	for surface, want := range expected {
		got := CSS(Default(), surface)
		if surface == "settings" {
			got = strings.Split(got, "\n.sixuan-settings .swatch")[0]
		}
		if surface == "candidates" {
			got = got[strings.Index(got, "\n")+1:]
			want = strings.ReplaceAll(want, "sixuan-candidates", CandidateScope+"."+ThemedClass)
		}
		if canonical(got) != canonical(want) {
			t.Errorf("default %s CSS changed", surface)
		}
	}
}
