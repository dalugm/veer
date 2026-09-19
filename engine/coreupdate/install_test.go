package coreupdate

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func installFixture(t *testing.T) (*Client, Release, string) {
	t.Helper()
	c := New()
	c.goos, c.goarch = "linux", "amd64"
	target := filepath.Join(t.TempDir(), "xray.exe")
	if err := os.WriteFile(target, []byte("old executable"), 0o751); err != nil {
		t.Fatal(err)
	}
	c.version = func(_ context.Context, path string) (string, error) {
		if strings.Contains(path, "next") {
			return "Xray 1.1.0", nil
		}
		return "Xray 1.0.0", nil
	}
	archive := zipFixture(t, "xray", "new executable")
	r := Release{
		Version:   "v1.1.0",
		tag:       "v1.1.0",
		binary:    asset{Name: "Xray-linux-64.zip", Size: int64(len(archive))},
		checksums: asset{Name: "Xray-linux-64.zip.dgst", Size: 299},
	}
	c.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://github.com/XTLS/Xray-core/releases/download/v1.1.0/Xray-linux-64.zip.dgst":
			return response(200, fmt.Sprintf("SHA2-256= %x\n", sha256.Sum256(archive))), nil
		case "https://github.com/XTLS/Xray-core/releases/download/v1.1.0/Xray-linux-64.zip":
			return response(200, string(archive)), nil
		default:
			t.Fatalf("unexpected download: %s", req.URL)
			return nil, errors.New("unexpected URL")
		}
	})
	return c, r, target
}

func assertContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s: got %q, %v; want %q", path, got, err, want)
	}
}

func TestInstallVerifiedBinaryPreservesMode(t *testing.T) {
	c, r, target := installFixture(t)
	result, err := c.Install(t.Context(), target, "v1.0.0", r)
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "v1.1.0" {
		t.Fatalf("result: %+v", result)
	}
	assertContents(t, target, "new executable")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o751 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil || len(entries) != 2 {
		t.Fatalf("expected executable and retained backup directory: %v %v", entries, err)
	}
	assertContents(t, result.BackupPath, "old executable")
}

func TestInstallPreservesRunningProcess(t *testing.T) {
	if os.Getenv("VEER_UPDATE_TEST_CHILD") == "1" {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
		os.Exit(0)
	}
	c, release, target := installFixture(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o751); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, target, "-test.run=^TestInstallPreservesRunningProcess$")
	cmd.Env = append(os.Environ(), "VEER_UPDATE_TEST_CHILD=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = in.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	}()
	scanner := bufio.NewScanner(out)
	ping := func(message string) {
		t.Helper()
		if _, err := fmt.Fprintln(in, message); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() || scanner.Text() != message {
			t.Fatalf("running process stopped responding: %v", scanner.Err())
		}
	}
	ping("before update")
	if _, err := c.Install(ctx, target, "v1.0.0", release); err != nil {
		t.Fatal(err)
	}
	assertContents(t, target, "new executable")
	ping("after update")
}

func TestInstallFailuresPreserveExistingBinary(t *testing.T) {
	for _, kind := range []string{"checksum", "duplicate checksum", "missing checksum", "http", "short", "long", "cancel", "changed target", "digest", "downgrade", "invalid release"} {
		t.Run(kind, func(t *testing.T) {
			c, r, target := installFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			original := c.http.Transport
			c.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, ".zip.dgst") {
					switch kind {
					case "checksum":
						return response(200, "SHA256= "+strings.Repeat("0", 64)), nil
					case "missing checksum":
						return response(200, "SHA512= "+strings.Repeat("0", 128)), nil
					case "duplicate checksum":
						line := fmt.Sprintf(
							"SHA256= %x\n",
							sha256.Sum256([]byte("new executable")),
						)
						return response(200, line+line), nil
					}
				} else {
					switch kind {
					case "http":
						return response(503, "failed"), nil
					case "short":
						return response(200, "new"), nil
					case "long":
						return response(200, strings.Repeat("x", int(r.binary.Size)+1)), nil
					case "cancel":
						cancel()
					case "changed target":
						if err := os.Rename(target, target+".moved"); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(
							target,
							[]byte("another executable"),
							0o751,
						); err != nil {
							t.Fatal(err)
						}
					}
				}
				return original.RoundTrip(req)
			})
			current := "v1.0.0"
			if kind == "digest" {
				r.binary.Digest = "sha256:" + strings.Repeat("0", 64)
			}
			if kind == "downgrade" {
				current = "v2.0.0"
			}
			if kind == "invalid release" {
				r.tag = "../../other"
			}
			if _, err := c.Install(ctx, target, current, r); err == nil {
				t.Fatal("failed install reported success")
			}
			if kind == "changed target" {
				assertContents(t, target, "another executable")
				assertContents(t, target+".moved", "old executable")
			} else {
				assertContents(t, target, "old executable")
			}
			entries, err := os.ReadDir(filepath.Dir(target))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".veer-update-") {
					t.Fatalf("staging leaked: %s", e.Name())
				}
			}
		})
	}
}

func TestInstallRejectsCancelledWorkBeforeDiskOrNetwork(t *testing.T) {
	c, r, target := installFixture(t)
	c.version = func(context.Context, string) (string, error) {
		t.Fatal("cancelled work inspected disk")
		return "", nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Install(ctx, target, "v1.0.0", r); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertContents(t, target, "old executable")
}

func TestInstallFollowsLocalExecutableSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	c, r, target := installFixture(t)
	link := filepath.Join(t.TempDir(), "veer")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Install(t.Context(), link, "v1.0.0", r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("symlink replaced: %v", err)
	}
	assertContents(t, target, "new executable")
}

func TestReplacementRollbackAndRecovery(t *testing.T) {
	for _, failAt := range []string{"backup", "publish", "rollback", "none"} {
		t.Run(failAt, func(t *testing.T) {
			dir := t.TempDir()
			target, staged, backup := filepath.Join(
				dir,
				"veer",
			), filepath.Join(
				dir,
				"new",
			), filepath.Join(
				dir,
				"old",
			)
			if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(staged, []byte("new"), 0o700); err != nil {
				t.Fatal(err)
			}
			failed := errors.New("rename failed")
			rename := func(from, to string) error {
				if (failAt == "backup" && from == target) ||
					((failAt == "publish" || failAt == "rollback") && from == staged) ||
					(failAt == "rollback" && from == backup) {
					return failed
				}
				return os.Rename(from, to)
			}
			err := replaceWithBackup(staged, target, backup, rename)
			if failAt == "none" {
				if err != nil {
					t.Fatal(err)
				}
				assertContents(t, target, "new")
				return
			}
			if !errors.Is(err, failed) {
				t.Fatalf("lost cause: %v", err)
			}
			if failAt == "rollback" {
				assertContents(t, backup, "old")
				if !strings.Contains(err.Error(), backup) {
					t.Fatalf("no recovery path: %v", err)
				}
			} else {
				assertContents(t, target, "old")
			}
		})
	}
}

func TestReleaseRedirectRejectsUntrustedDestinations(t *testing.T) {
	for _, address := range []string{"http://github.com/x", "https://evil.example/x", "https://github.com.evil.example/x", "https://user@github.com/x", "https://github.com:123/x"} {
		req, _ := http.NewRequest(http.MethodGet, address, nil)
		if err := releaseRedirect(req, nil); err == nil {
			t.Fatalf("allowed %s", address)
		}
	}
	for _, address := range []string{"https://github.com/x", "https://release-assets.githubusercontent.com/x"} {
		req, _ := http.NewRequest(http.MethodGet, address, nil)
		if err := releaseRedirect(req, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChecksumResponseLimit(t *testing.T) {
	c, r, target := installFixture(t)
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxChecksumsSize+1))),
		}, nil
	})
	if _, err := c.Install(t.Context(), target, "v1.0.0", r); err == nil {
		t.Fatal("oversized checksums accepted")
	}
	assertContents(t, target, "old executable")
}

func zipFixture(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range []struct{ name, body string }{{name, content}, {"geoip.dat", "must stay untouched"}} {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallRejectsUnsafeArchiveOrWrongCore(t *testing.T) {
	for _, kind := range []string{"missing", "traversal", "symlink", "duplicate", "wrong version", "not zip", "wrong prerelease", "wrong stable tag"} {
		t.Run(kind, func(t *testing.T) {
			c, r, target := installFixture(t)
			name := "xray"
			if kind == "missing" {
				name = "unrelated"
			}
			if kind == "traversal" {
				name = "../xray"
			}
			data := zipFixture(t, name, "new executable")
			if kind == "not zip" {
				data = []byte("not an archive")
			}
			if kind == "symlink" || kind == "duplicate" {
				var buf bytes.Buffer
				zw := zip.NewWriter(&buf)
				for range 2 {
					h := &zip.FileHeader{Name: "xray"}
					if kind == "symlink" {
						h.SetMode(os.ModeSymlink | 0o777)
					}
					w, err := zw.CreateHeader(h)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = w.Write([]byte("new executable")); err != nil {
						t.Fatal(err)
					}
				}
				if err := zw.Close(); err != nil {
					t.Fatal(err)
				}
				data = buf.Bytes()
			}
			if kind == "wrong version" {
				c.version = func(context.Context, string) (string, error) { return "Xray 1.0.0", nil }
			}
			if kind == "wrong prerelease" || kind == "wrong stable tag" {
				r.Version, r.tag = "v1.1.0-rc.10", "v1.1.0-rc.10"
				c.version = func(_ context.Context, path string) (string, error) {
					if filepath.Base(path) == filepath.Base(target) {
						return "Xray 1.0.0", nil
					}
					if kind == "wrong stable tag" {
						return "Xray 1.1.0 (Xray, Penetrates Everything.) v1.1.0 (go1.26 linux/amd64)", nil
					}
					return "Xray 1.1.0 (Xray, Penetrates Everything.) v1.1.0-rc.1 (go1.26 linux/amd64)", nil
				}
			}
			r.binary.Size = int64(len(data))
			c.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, ".dgst") {
					return response(200, fmt.Sprintf("SHA256= %x", sha256.Sum256(data))), nil
				}
				return response(200, string(data)), nil
			})
			if _, err := c.Install(t.Context(), target, "v1.0.0", r); err == nil {
				t.Fatal("invalid archive/core installed")
			}
			assertContents(t, target, "old executable")
		})
	}
}
