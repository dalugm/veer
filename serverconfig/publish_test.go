package serverconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicationFailureRollsBackEveryDestination(t *testing.T) {
	for failure := range 5 {
		t.Run(fmt.Sprintf("publish-%d", failure), func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			var files []pendingFile
			previous := map[string]string{}
			for i, name := range []string{"client/tun.json", "client/proxy.json", "client/link.txt", "server.json.bak", "server.json"} {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if i != 1 {
					previous[path] = "old " + name
					if err := os.WriteFile(path, []byte(previous[path]), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				files = append(files, pendingFile{path: path, data: []byte("new " + name)})
			}
			injected := errors.New("publication denied")
			err = publishFiles(files, func(from, to string) error {
				if to == files[failure].path && strings.HasPrefix(filepath.Base(from), "next-") {
					return injected
				}
				return os.Rename(from, to)
			})
			if !errors.Is(err, injected) {
				t.Fatalf("lost publication failure: %v", err)
			}
			for _, f := range files {
				data, err := os.ReadFile(f.path)
				if expected, existed := previous[f.path]; existed {
					if err != nil || string(data) != expected {
						t.Fatalf("rollback lost %s: %q %v", f.path, data, err)
					}
				} else if !os.IsNotExist(err) {
					t.Fatalf("new file retained: %s", f.path)
				}
			}
		})
	}
}

func TestFailedRollbackRetainsRecoveryFiles(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client, server := filepath.Join(dir, "client.json"), filepath.Join(dir, "server.json")
	if err := os.WriteFile(client, []byte("original client"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(server, []byte("original server"), 0o600); err != nil {
		t.Fatal(err)
	}
	denied := errors.New("rename denied")
	err = publishFiles([]pendingFile{
		{path: client, data: []byte("new client")},
		{path: server, data: []byte("new server")},
	}, func(from, to string) error {
		if (to == server && strings.HasPrefix(filepath.Base(from), "next-")) ||
			(to == client && strings.HasPrefix(filepath.Base(from), "previous-")) {
			return denied
		}
		return os.Rename(from, to)
	})
	if err == nil || !strings.Contains(err.Error(), "recovery files in") {
		t.Fatalf("lost recovery location: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, ".veer-write-*", "previous-0"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("lost original client backup: %v", err)
	}
	data, err := os.ReadFile(backups[0])
	if err != nil || string(data) != "original client" {
		t.Fatalf("backup: %q %v", data, err)
	}
	data, err = os.ReadFile(server)
	if err != nil || string(data) != "original server" {
		t.Fatalf("server was not restored: %q %v", data, err)
	}
}

func TestPublicationRejectsChangedServerAndAliasedTargets(t *testing.T) {
	for _, alias := range []bool{false, true} {
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "server.json")
		if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
			t.Fatal(err)
		}
		files := []pendingFile{{path: path, data: []byte("new"), expected: []byte("stale")}}
		if alias {
			files[0].expected = nil
			files = append(
				files,
				pendingFile{path: filepath.Join(dir, ".", "server.json"), data: []byte("other")},
			)
		}
		if err = publishFiles(files, os.Rename); err == nil {
			t.Fatal("unsafe publication accepted")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "current" {
			t.Fatalf("source changed: %q %v", data, err)
		}
	}
}
