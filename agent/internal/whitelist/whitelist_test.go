package whitelist

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestReadBreakGlass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "breakglass")
	content := "root\n# a comment line\n  deploy   # trailing comment\n\nops\r\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := ReadBreakGlass(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, u := range []string{"root", "deploy", "ops"} {
		if !set.Contains(u) {
			t.Errorf("%q should be on the list", u)
		}
	}
	for _, u := range []string{"comment", "", "a"} {
		if set.Contains(u) {
			t.Errorf("%q should not be on the list", u)
		}
	}
	if len(set) != 3 {
		t.Errorf("got %d entries, want 3: %v", len(set), set)
	}
}

func TestReadBreakGlassMissingFile(t *testing.T) {
	set, err := ReadBreakGlass(filepath.Join(t.TempDir(), "none"))
	if err != nil || len(set) != 0 {
		t.Fatalf("missing file: got %v, %v; want an empty set and no error", set, err)
	}
}

func TestReadBreakGlassInsecure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not checked on Windows")
	}
	path := filepath.Join(t.TempDir(), "breakglass")
	if err := os.WriteFile(path, []byte("root\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	set, err := ReadBreakGlass(path)
	if !errors.Is(err, ErrInsecure) {
		t.Fatalf("got %v, want ErrInsecure", err)
	}
	if set.Contains("root") {
		t.Error("an insecure file must be ignored")
	}
}

func TestCacheAllows(t *testing.T) {
	now := time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	c := Cache{Entries: []Entry{
		{Username: "deploy", Context: "ssh"},
		{Username: "ops", Context: "ssh", ExpiresAt: &past},
		{Username: "ops", Context: "sudo", ExpiresAt: &future},
		{Username: "edge", Context: "ssh", ExpiresAt: &now},
	}}
	cases := []struct {
		user, context string
		want          bool
	}{
		{"deploy", "ssh", true},
		{"deploy", "sudo", false},
		{"ops", "ssh", false},
		{"ops", "sudo", true},
		{"edge", "ssh", false},
		{"nobody", "ssh", false},
	}
	for _, tc := range cases {
		if got := c.Allows(tc.user, tc.context, now); got != tc.want {
			t.Errorf("Allows(%q, %q) = %v, want %v", tc.user, tc.context, got, tc.want)
		}
	}
	if c.Allows("ops", "ssh", past.Add(-time.Second)) != true {
		t.Error("an entry is valid before its expiry")
	}
}

func TestSaveLoadCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "var", "lib", "ssh-sentinel", "whitelist.json")
	exp := time.Date(2026, 9, 14, 21, 12, 7, 0, time.UTC)
	in := Cache{
		SyncedAt: time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC),
		Entries:  []Entry{{Username: "deploy", Context: "ssh"}, {Username: "ops", Context: "sudo", ExpiresAt: &exp}},
	}
	if err := SaveCache(path, in); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Error("temp file left behind")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("cache mode = %o, want 0600", info.Mode().Perm())
		}
	}

	out, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if !out.SyncedAt.Equal(in.SyncedAt) || len(out.Entries) != 2 {
		t.Fatalf("round trip lost data: %+v", out)
	}
	if out.Entries[0].Username != "deploy" || out.Entries[0].ExpiresAt != nil {
		t.Errorf("first entry = %+v", out.Entries[0])
	}
	if out.Entries[1].ExpiresAt == nil || !out.Entries[1].ExpiresAt.Equal(exp) {
		t.Errorf("second entry expiry = %v", out.Entries[1].ExpiresAt)
	}

	// A second save replaces the file in place.
	if err := SaveCache(path, Cache{}); err != nil {
		t.Fatalf("second SaveCache: %v", err)
	}
	out, _ = LoadCache(path)
	if len(out.Entries) != 0 {
		t.Errorf("second save did not replace the file: %+v", out)
	}
}

func TestLoadCacheMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	c, err := LoadCache(filepath.Join(dir, "none.json"))
	if err != nil || len(c.Entries) != 0 {
		t.Fatalf("missing file: got %+v, %v", c, err)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	if _, err := LoadCache(bad); err == nil {
		t.Fatal("corrupt cache must return an error")
	}
}
