package integrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)

	if got, err := store.Load(); err != nil || got != nil {
		t.Fatalf("a missing file must read as empty: %v %v", got, err)
	}

	configs := []Config{
		{ID: "work-cal", Kind: "ics", Name: "Рабочий календарь", Enabled: true,
			Interval: 15 * time.Minute, Values: map[string]string{"url": "https://example.com/c.ics"}},
		{ID: "issues", Kind: "github", Name: "Issues", Enabled: false,
			Interval: time.Hour, Folder: "70-integrations/issues",
			Values: map[string]string{"repo": "a/b", "token": "bitwarden:gh"}},
	}
	if err := store.Save(configs); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d", len(loaded))
	}
	if loaded[0].ID != "issues" || loaded[1].ID != "work-cal" {
		t.Errorf("expected id order, got %s %s", loaded[0].ID, loaded[1].ID)
	}
	if loaded[1].Interval != 15*time.Minute {
		t.Errorf("interval = %v", loaded[1].Interval)
	}
	if loaded[0].Values["token"] != "bitwarden:gh" {
		t.Errorf("values not preserved: %v", loaded[0].Values)
	}

	// The file must stay human-editable, with a warning about secrets.
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ConfigFile)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "interval: 15m0s") {
		t.Errorf("interval should be written as a duration string:\n%s", raw)
	}
	if !strings.Contains(string(raw), "bitwarden:") {
		t.Errorf("header should explain secret references:\n%s", raw)
	}
}

func TestStoreClampsInterval(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Save([]Config{{ID: "a", Kind: "ics", Interval: time.Second}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].Interval < MinInterval {
		t.Errorf("interval = %v, want at least %v", loaded[0].Interval, MinInterval)
	}
}

func TestStateRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	if got := store.LoadState(); len(got) != 0 {
		t.Fatalf("expected empty state, got %v", got)
	}

	now := time.Now().Truncate(time.Second)
	state := map[string]State{
		"work-cal": {Cursor: map[string]string{"etag": `"v1"`}, LastSync: now, LastItems: 42},
	}
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	got := store.LoadState()
	if got["work-cal"].Cursor["etag"] != `"v1"` || got["work-cal"].LastItems != 42 {
		t.Errorf("state = %+v", got["work-cal"])
	}
}

func TestNewIDIsUnique(t *testing.T) {
	taken := []Config{{ID: "rabochiy"}, {ID: "issues"}}
	if id := NewID("Issues", taken); id != "issues-2" {
		t.Errorf("id = %q, want issues-2", id)
	}
	// Identifiers travel through URLs and folder names, so they are ASCII.
	if id := NewID("Рабочий календарь", taken); id != "rabochiy-kalendar" {
		t.Errorf("id = %q, want a transliterated slug", id)
	}
	if id := NewID("Мой  --  Календарь!", nil); id != "moy-kalendar" {
		t.Errorf("id = %q", id)
	}
}

func TestDefaultFolder(t *testing.T) {
	if got := DefaultFolder(Config{ID: "issues"}); got != "70-integrations/issues" {
		t.Errorf("folder = %q", got)
	}
	if got := DefaultFolder(Config{ID: "issues", Folder: "/work/gh/"}); got != "work/gh" {
		t.Errorf("folder = %q", got)
	}
}
