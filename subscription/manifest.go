package subscription

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dalugm/veer/engine"
)

const defaultOutboundTag = "out-vless"

// Manifest describes the server and users used to generate client bundles.
type Manifest struct {
	ClientTemplate string          `json:"clientTemplate"`
	OutboundTag    string          `json:"outboundTag"`
	Label          string          `json:"label"`
	Reality        ManifestReality `json:"reality"`
	Users          []ManifestUser  `json:"users"`
}

// ManifestReality contains the server REALITY parameters exposed to clients.
type ManifestReality struct {
	ShortID string `json:"shortId"`
}

// ManifestUser describes one user and the labels for their generated bundle.
type ManifestUser struct {
	Name  string `json:"name"`
	ID    string `json:"id"`
	Token string `json:"token"`
	Label string `json:"label,omitempty"`
}

func readManifest(path string) (*Manifest, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, "", fmt.Errorf("parse manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, "", fmt.Errorf("parse manifest: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", fmt.Errorf("resolve manifest path: %w", err)
	}
	return &manifest, filepath.Dir(absPath), nil
}

func validateManifest(manifest *Manifest, baseDir string) error {
	if manifest.OutboundTag == "" {
		manifest.OutboundTag = defaultOutboundTag
	}
	if strings.TrimSpace(manifest.Label) == "" {
		return errors.New("manifest label must not be empty")
	}
	if strings.TrimSpace(manifest.ClientTemplate) == "" {
		return errors.New("manifest clientTemplate must not be empty")
	}
	if err := engine.ValidateRealityShortID(manifest.Reality.ShortID); err != nil {
		return fmt.Errorf("reality.shortId: %w", err)
	}
	if len(manifest.Users) == 0 {
		return errors.New("manifest contains no users")
	}
	templateData, err := os.ReadFile(resolveConfigPath(baseDir, manifest.ClientTemplate))
	if err != nil {
		return fmt.Errorf("read client template: %w", err)
	}

	names := make(map[string]struct{}, len(manifest.Users))
	ids := make(map[string]struct{}, len(manifest.Users))
	tokens := make(map[string]struct{}, len(manifest.Users))
	for i, user := range manifest.Users {
		prefix := fmt.Sprintf("users[%d]", i)
		if strings.TrimSpace(user.Name) == "" {
			return fmt.Errorf("%s.name must not be empty", prefix)
		}
		if _, exists := names[user.Name]; exists {
			return fmt.Errorf("duplicate user name %q", user.Name)
		}
		names[user.Name] = struct{}{}
		if strings.TrimSpace(user.ID) == "" {
			return fmt.Errorf("%s.id must not be empty", prefix)
		}
		if _, exists := ids[user.ID]; exists {
			return fmt.Errorf("duplicate id for user %q", user.Name)
		}
		ids[user.ID] = struct{}{}
		if err := validateToken(user.Token); err != nil {
			return fmt.Errorf("%s.token: %w", prefix, err)
		}
		if _, exists := tokens[user.Token]; exists {
			return fmt.Errorf("duplicate token for user %q", user.Name)
		}
		tokens[user.Token] = struct{}{}

		_, _, config, err := buildClientConfigs(
			templateData,
			manifest.OutboundTag,
			manifest.Reality.ShortID,
			user.ID,
		)
		if err != nil {
			return fmt.Errorf("user %q: %w", user.Name, err)
		}
		label := user.Label
		if label == "" {
			label = manifest.Label
		}
		if _, err := config.ToVLESSLink(manifest.OutboundTag, label); err != nil {
			return fmt.Errorf("user %q: %w", user.Name, err)
		}
	}
	return nil
}

func resolveConfigPath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func validateToken(token string) error {
	if len(token) != 64 {
		return errors.New("must be 64 lowercase hexadecimal characters")
	}
	decoded, err := hex.DecodeString(token)
	if err != nil || hex.EncodeToString(decoded) != token || len(decoded) != 32 {
		return errors.New("must be 64 lowercase hexadecimal characters")
	}
	return nil
}

func newToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(random), nil
}
