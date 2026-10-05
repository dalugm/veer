package privilege

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHelperArchiveAppendsOnlyToVerifiedUserFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.log")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := currentLogReader()
	if err != nil {
		t.Fatal(err)
	}
	file, err := openSessionArchive(path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("appended\n"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original\nappended\n" {
		t.Fatalf("%q %v", data, err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err == nil {
		if f, err := openSessionArchive(link, reader); err == nil {
			_ = f.Close()
			t.Fatal("accepted symlink")
		}
	}
	if _, err := openSessionArchive(path+".missing", reader); err == nil {
		t.Fatal("created archive inside helper")
	}
}

func TestManagerClosesAndRemovesSessionArchive(t *testing.T) {
	m := New()
	f, err := os.CreateTemp(t.TempDir(), "session-*.log")
	if err != nil {
		t.Fatal(err)
	}
	m.archive = f
	if m.Snapshot().LogArchive != f.Name() {
		t.Fatal("archive path not available for search")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Fatalf("archive retained: %v", err)
	}
	if m.Snapshot().LogArchive != "" {
		t.Fatal("closed archive still exposed")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}
