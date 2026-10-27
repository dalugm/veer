package profile

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dalugm/veer/download"
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/settings"
)

const templateJSON = `{"log":{"access":"none","error":"none"},"dns":{"servers":["223.5.5.5"]},"inbounds":[{"protocol":"mixed","port":10808}],"routing":{"rules":[{"outboundTag":"proxy","domain":["geosite:geolocation-!cn"]}]},"outbounds":[{"protocol":"vless","tag":"proxy","settings":{},"streamSettings":{"network":"tcp","sockopt":{"domainStrategy":"ForceIPv4"}}},{"protocol":"freedom","tag":"direct"}]}`

func nodeLink(name, host string) string {
	return "vless://11111111-2222-4333-8444-555555555555@" + host + ":443?type=tcp&security=tls&sni=example.com#" + name
}

func TestSyncDefaultProfilesRefreshBootstrapAndChangeMode(t *testing.T) {
	for _, mode := range []string{"mixed", "tun"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "veer", "settings.json")
			host := "old.example"
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = fmt.Fprintln(w, nodeLink("Node", host))
				}),
			)
			defer server.Close()
			source := settings.Subscription{Name: "Home", URL: server.URL, Mode: mode}
			c, err := (Client{}).Sync(t.Context(), path, settings.Defaults(), source, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			first := c.Profiles[0]
			before, err := os.ReadFile(first.Path)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Dir(first.Path) != filepath.Join(filepath.Dir(path), "profiles") ||
				!strings.Contains(string(before), "full:old.example") {
				t.Fatal("default import used a template or omitted bootstrap")
			}
			if info, err := os.Stat(first.Path); err != nil || info.Mode().Perm()&0o077 != 0 {
				t.Fatal("node credentials are not private")
			}
			loaded, err := settings.Load(path)
			if err != nil || loaded.Subscriptions[0].ConfigMode() != mode ||
				loaded.Subscriptions[0].Template != "" {
				t.Fatal("default mode did not survive reload", err)
			}
			host = "new.example"
			source = c.Subscriptions[0]
			updated, err := (Client{}).Sync(t.Context(), path, c, source, first.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(updated.Profiles[0].Path)
			retained, _ := os.ReadFile(first.Path)
			if updated.Profiles[0].ID != first.ID || !bytes.Equal(before, retained) ||
				strings.Contains(
					string(data),
					"old.example",
				) || !strings.Contains(string(data), "full:new.example") {
				t.Fatal("refresh retained stale bootstrap or replaced the running configuration")
			}
			source.Mode = "tun"
			if mode == "tun" {
				source.Mode = "mixed"
			}
			changed, err := (Client{}).Sync(t.Context(), path, updated, source, first.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			info, err := engine.Inspect(changed.Profiles[0].Path)
			if err != nil || info.TUN != (source.Mode == "tun") || changed.Selected != first.ID {
				t.Fatal(
					"mode change did not preserve selection and produce requested listeners",
					err,
				)
			}
		})
	}
}

func TestSyncDefaultRollbackRemovesStagedFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, nodeLink("../CON", "node.example"))
	}))
	defer server.Close()
	_, err := (Client{}).Sync(
		t.Context(),
		path,
		settings.Defaults(),
		settings.Subscription{Name: "Home", URL: server.URL},
		"",
		nil,
	)
	if err == nil {
		t.Fatal("published over a settings directory")
	}
	files, _ := filepath.Glob(filepath.Join(root, "profiles", "*"))
	if len(files) != 0 {
		t.Fatal("failed default import leaked staged credentials", files)
	}
}

func fixture(t *testing.T) (string, settings.Subscription) {
	t.Helper()
	dir := t.TempDir()
	template := filepath.Join(dir, "template.json")
	if err := os.WriteFile(template, []byte(templateJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(
		dir,
		"settings.json",
	), settings.Subscription{
		Name:     "Home",
		Template: template,
	}
}

func TestSyncImportsRefreshesAndKeepsOriginalFiles(t *testing.T) {
	path, source := fixture(t)
	body := nodeLink("One", "old.example") + "\n" + nodeLink("Two", "two.example")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, base64.StdEncoding.EncodeToString([]byte(body)))
	}))
	defer server.Close()
	source.URL = server.URL + "/secret-token"
	var progress download.Progress
	c, err := (Client{}).Sync(
		t.Context(),
		path,
		settings.Defaults(),
		source,
		"",
		func(p download.Progress) { progress = p },
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 2 || len(c.Subscriptions) != 1 || !progress.Done {
		t.Fatal("incomplete import or progress")
	}
	first := c.Profiles[0]
	original, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(first.Path) != filepath.Dir(source.Template) ||
		!strings.Contains(string(original), "old.example") {
		t.Fatal("wrong path or node")
	}
	if info, err := os.Stat(first.Path); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatal("credentials not private")
	}
	source = c.Subscriptions[0]
	unchanged, err := (Client{}).Sync(t.Context(), path, c, source, first.ID, nil)
	if err != nil || unchanged.Profiles[0].Path != first.Path {
		t.Fatal("unchanged refresh rewrote files", err)
	}
	body = nodeLink("Two", "two.example") + "\n" + nodeLink("One", "new.example")
	updated, err := (Client{}).Sync(t.Context(), path, c, source, first.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Selected != first.ID || updated.Profiles[1].ID != first.ID ||
		updated.Profiles[1].Path == first.Path {
		t.Fatal("selection or identity lost on refresh")
	}
	oldData, _ := os.ReadFile(first.Path)
	if !bytes.Equal(oldData, original) {
		t.Fatal("overwrote active/backup configuration")
	}
	template, _ := os.ReadFile(source.Template)
	if string(template) != templateJSON {
		t.Fatal("overwrote local template")
	}
	loaded, err := settings.Load(path)
	if err != nil || len(loaded.Profiles) != 2 || loaded.Subscriptions[0].URL != source.URL {
		t.Fatal("subscription did not survive reload", err)
	}
	body = nodeLink("Two", "two.example")
	before, _ := os.ReadFile(path)
	filesBefore, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".veer-sub-*.json"))
	if _, err := (Client{}).Sync(t.Context(), path, updated, source, first.ID, nil); err == nil {
		t.Fatal("removed running profile")
	}
	after, _ := os.ReadFile(path)
	filesAfter, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".veer-sub-*.json"))
	if !bytes.Equal(before, after) || len(filesBefore) != len(filesAfter) {
		t.Fatal("failed refresh changed disk")
	}
	pruned, err := (Client{}).Sync(t.Context(), path, updated, source, "", nil)
	if err != nil || len(pruned.Profiles) != 1 || pruned.Selected != pruned.Profiles[0].ID {
		t.Fatal("offline pruning failed", err)
	}
}

func TestSyncRollsBackStagedFilesWhenSettingsCannotPublish(t *testing.T) {
	path, source := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, nodeLink("../CON", "example.com"))
	}))
	defer server.Close()
	source.URL = server.URL
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := (Client{}).Sync(t.Context(), path, settings.Defaults(), source, "", nil)
	if err == nil {
		t.Fatal("published settings over directory")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".veer-sub-*"))
	if len(files) != 0 {
		t.Fatal("failed import leaked staged credential files")
	}
}

func TestFetchErrorsAreBoundedAndDoNotExposeURLs(t *testing.T) {
	for _, tc := range []struct {
		code int
		body string
	}{{http.StatusUnauthorized, "secret-token"}, {http.StatusOK, "<html>secret-token</html>"}, {http.StatusOK, strings.Repeat("x", 4<<20+1)}} {
		server := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.code); _, _ = fmt.Fprint(w, tc.body) },
			),
		)
		_, err := (Client{}).fetch(t.Context(), server.URL+"/secret-token", nil)
		server.Close()
		if err == nil || strings.Contains(err.Error(), "secret-token") ||
			strings.Contains(err.Error(), server.URL) {
			t.Fatal("accepted invalid response or leaked URL")
		}
	}
}

func TestFetchCancellationAndHTTPSDowngrade(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(
		http.HandlerFunc(
			func(_ http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() },
		),
	)
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { _, err := (Client{}).fetch(ctx, server.URL, nil); result <- err }()
	<-entered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("did not cancel download", err)
	}
	redirect := httptest.NewTLSServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://127.0.0.1:1/secret-token", http.StatusFound)
		}),
	)
	defer redirect.Close()
	_, err := (Client{HTTP: redirect.Client()}).fetch(
		t.Context(),
		redirect.URL+"/secret-token",
		nil,
	)
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatal("accepted downgrade or leaked redirect URL")
	}
}
