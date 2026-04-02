package main

import (
	"testing"
	"testing/fstest"
)

func TestLoadConfigs_ReturnsConfFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"config/app.conf":  {Data: []byte("key=value")},
		"config/db.conf":   {Data: []byte("host=localhost")},
		"config/readme.md": {Data: []byte("ignore me")},
	}

	result, err := LoadConfigs(fsys, "config")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}

	cases := map[string]string{
		"config/app.conf": "key=value",
		"config/db.conf":  "host=localhost",
	}
	for path, want := range cases {
		got, ok := result[path]
		if !ok {
			t.Errorf("missing key %q", path)
			continue
		}
		if string(got) != want {
			t.Errorf("path %q: got %q, want %q", path, got, want)
		}
	}
}

func TestLoadConfigs_WalksSubdirectories(t *testing.T) {
	fsys := fstest.MapFS{
		"root/a.conf":            {Data: []byte("a")},
		"root/sub/b.conf":        {Data: []byte("b")},
		"root/sub/deep/c.conf":   {Data: []byte("c")},
		"root/sub/deep/note.txt": {Data: []byte("skip")},
	}

	result, err := LoadConfigs(fsys, "root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"root/a.conf",
		"root/sub/b.conf",
		"root/sub/deep/c.conf",
	}
	if len(result) != len(expected) {
		t.Fatalf("expected %d entries, got %d", len(expected), len(result))
	}
	for _, p := range expected {
		if _, ok := result[p]; !ok {
			t.Errorf("missing expected path %q", p)
		}
	}
}

func TestLoadConfigs_ExcludesNonConfFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"cfg/settings.json": {Data: []byte("{}")},
		"cfg/notes.txt":     {Data: []byte("notes")},
		"cfg/only.conf":     {Data: []byte("ok")},
	}

	result, err := LoadConfigs(fsys, "cfg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(result), result)
	}
	if _, ok := result["cfg/only.conf"]; !ok {
		t.Error("expected cfg/only.conf to be present")
	}
}

func TestLoadConfigs_EmptyDirectory(t *testing.T) {
	fsys := fstest.MapFS{
		"empty/.keep": {Data: []byte("")},
	}

	result, err := LoadConfigs(fsys, "empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}
}

func TestLoadConfigs_InvalidRootReturnsError(t *testing.T) {
	fsys := fstest.MapFS{
		"config/app.conf": {Data: []byte("x=1")},
	}

	_, err := LoadConfigs(fsys, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent root, got nil")
	}
}

func TestLoadConfigs_ContentIsPreserved(t *testing.T) {
	content := "multiline\ncontent\nhere\n"
	fsys := fstest.MapFS{
		"dir/multi.conf": {Data: []byte(content)},
	}

	result, err := LoadConfigs(fsys, "dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := result["dir/multi.conf"]
	if !ok {
		t.Fatal("expected dir/multi.conf in result")
	}
	if string(got) != content {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}
