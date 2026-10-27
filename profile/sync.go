package profile

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dalugm/veer/download"
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/settings"
)

// Sync stages every node before atomically publishing settings. Generated files
// live beside the template so relative assets, log paths and core working
// directories retain their meaning. Old configurations remain as local backups.
// protectedID prevents removing the profile of a still-running connection.
func (client Client) Sync(
	ctx context.Context,
	path string,
	current settings.Config,
	source settings.Subscription,
	protectedID string,
	report func(download.Progress),
) (settings.Config, error) {
	if err := ctx.Err(); err != nil {
		return current, err
	}
	source.Name, source.URL = strings.TrimSpace(source.Name), strings.TrimSpace(source.URL)
	source.OutboundTag = strings.TrimSpace(source.OutboundTag)
	if source.Name == "" {
		return current, errors.New("enter a subscription name")
	}
	if err := settings.ValidateSubscriptionURL(source.URL); err != nil {
		return current, err
	}
	var err error
	source.Template, err = filepath.Abs(settings.ExpandPath(source.Template))
	if err != nil {
		return current, errors.New("invalid template path")
	}
	for _, existing := range current.Subscriptions {
		if source.ID != existing.ID && source.URL == existing.URL {
			return current, errors.New("this subscription URL is already imported")
		}
	}
	if source.ID == "" {
		source.ID = rand.Text()
	} else if _, ok := current.Subscription(source.ID); !ok {
		return current, errors.New("subscription no longer exists")
	}
	template, err := readConfig(source.Template)
	if err != nil {
		return current, errors.New("cannot read the local Xray JSON template (maximum 8 MiB)")
	}
	// Validate the template before making any network request.
	if _, err := engine.Inspect(source.Template); err != nil {
		return current, errors.New("template must be a valid local Xray JSON configuration")
	}
	nodes, err := client.fetch(ctx, source.URL, report)
	if err != nil {
		return current, err
	}
	old := make(map[string]settings.Profile)
	for _, p := range current.Profiles {
		if p.SubscriptionID == source.ID {
			old[p.NodeKey] = p
		}
	}
	var created []string
	published := false
	defer func() {
		if !published {
			for _, p := range created {
				_ = os.Remove(p)
			}
		}
	}()
	next := current
	next.Subscriptions = slices.Clone(current.Subscriptions)
	next.Profiles = make([]settings.Profile, 0, len(current.Profiles)+len(nodes))
	for _, p := range current.Profiles {
		if p.SubscriptionID != source.ID {
			next.Profiles = append(next.Profiles, p)
		}
	}
	protectedFound := protectedID == ""
	for _, p := range next.Profiles {
		protectedFound = protectedFound || p.ID == protectedID
	}
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return current, err
		}
		native, err := json.Marshal(node.Outbound)
		if err != nil {
			return current, errors.New("cannot encode subscription node")
		}
		data, err := engine.ApplyProfileNode(template, native, source.OutboundTag)
		if err != nil {
			return current, err
		}
		hash := sha256.Sum256([]byte(node.Name))
		key := hex.EncodeToString(hash[:])
		p, found := old[key]
		if !found {
			p = settings.Profile{
				ID:             rand.Text(),
				Engine:         "xray",
				SubscriptionID: source.ID,
				NodeKey:        key,
			}
		}
		p.Name = source.Name + " / " + node.Name
		previous, readErr := readConfig(p.Path)
		if !found || readErr != nil || !bytes.Equal(previous, data) ||
			filepath.Dir(p.Path) != filepath.Dir(source.Template) {
			p.Path, err = stageConfig(filepath.Dir(source.Template), data)
			if err != nil {
				return current, err
			}
			created = append(created, p.Path)
		}
		next.Profiles = append(next.Profiles, p)
		protectedFound = protectedFound || p.ID == protectedID
	}
	if !protectedFound {
		return current, errors.New(
			"the subscription removed the active node; disconnect before refreshing",
		)
	}
	index := slices.IndexFunc(
		next.Subscriptions,
		func(s settings.Subscription) bool { return s.ID == source.ID },
	)
	if index < 0 {
		next.Subscriptions = append(next.Subscriptions, source)
	} else {
		next.Subscriptions[index] = source
	}
	if _, ok := next.Active(); !ok {
		next.Selected = next.Profiles[0].ID
	}
	if err := ctx.Err(); err != nil {
		return current, err
	}
	if err := settings.Save(path, next); err != nil {
		return current, fmt.Errorf("save imported profiles: %w", err)
	}
	published = true
	return next, nil
}

func readConfig(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
	if err == nil && len(data) > 8<<20 {
		err = errors.New("config exceeds 8 MiB")
	}
	return data, err
}

func stageConfig(dir string, data []byte) (string, error) {
	// CreateTemp supplies a private, exclusive name; remote labels never form paths.
	f, err := os.CreateTemp(dir, ".veer-sub-*.json")
	if err != nil {
		return "", errors.New(
			"cannot create a subscription config beside the template; choose a writable template directory",
		)
	}
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	err = errors.Join(writeErr, f.Close())
	if err == nil {
		_, err = engine.Inspect(f.Name())
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", errors.New("cannot write a valid subscription profile")
	}
	return f.Name(), nil
}
