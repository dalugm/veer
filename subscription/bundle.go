package subscription

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func generate(ctx context.Context, manifestPath, outputDir string) (int, error) {
	manifest, baseDir, err := readManifest(manifestPath)
	if err != nil {
		return 0, err
	}
	if err := validateManifest(manifest, baseDir); err != nil {
		return 0, err
	}
	templateData, err := os.ReadFile(resolveConfigPath(baseDir, manifest.ClientTemplate))
	if err != nil {
		return 0, fmt.Errorf("read client template: %w", err)
	}

	absOutput, err := safeOutputPath(outputDir)
	if err != nil {
		return 0, err
	}
	parent := filepath.Dir(absOutput)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return 0, fmt.Errorf("create output parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(absOutput)+".tmp-")
	if err != nil {
		return 0, fmt.Errorf("create staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := os.Chmod(stage, 0o700); err != nil {
		return 0, fmt.Errorf("secure staging directory: %w", err)
	}

	for _, user := range manifest.Users {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		proxyData, tunData, config, err := buildClientConfigs(
			templateData,
			manifest.OutboundTag,
			manifest.Reality.ShortID,
			user.ID,
		)
		if err != nil {
			return 0, fmt.Errorf("user %q: %w", user.Name, err)
		}
		label := user.Label
		if label == "" {
			label = manifest.Label
		}
		link, err := config.ToVLESSLink(manifest.OutboundTag, label)
		if err != nil {
			return 0, fmt.Errorf("user %q: %w", user.Name, err)
		}
		bundleDir := filepath.Join(stage, user.Token)
		if err := os.Mkdir(bundleDir, 0o700); err != nil {
			return 0, fmt.Errorf("create bundle for user %q: %w", user.Name, err)
		}
		if err := os.WriteFile(
			filepath.Join(bundleDir, "subscription.txt"),
			[]byte(link+"\n"),
			0o600,
		); err != nil {
			return 0, fmt.Errorf("write VLESS subscription for user %q: %w", user.Name, err)
		}
		if err := os.WriteFile(
			filepath.Join(bundleDir, "config-proxy.json"),
			proxyData,
			0o600,
		); err != nil {
			return 0, fmt.Errorf("write proxy client config for user %q: %w", user.Name, err)
		}
		if err := os.WriteFile(
			filepath.Join(bundleDir, "config-tun.json"),
			tunData,
			0o600,
		); err != nil {
			return 0, fmt.Errorf("write TUN client config for user %q: %w", user.Name, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := replaceDirectory(stage, absOutput); err != nil {
		return 0, err
	}
	return len(manifest.Users), nil
}

func safeOutputPath(outputDir string) (string, error) {
	if strings.TrimSpace(outputDir) == "" {
		return "", errors.New("output directory must not be empty")
	}
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}
	clean := filepath.Clean(absOutput)
	if clean == string(filepath.Separator) || filepath.Base(clean) == "." {
		return "", fmt.Errorf("refusing unsafe output directory %q", outputDir)
	}
	if filepath.Base(clean) != "subscriptions" {
		return "", fmt.Errorf("output directory must be named subscriptions, got %q", outputDir)
	}
	return clean, nil
}

func replaceDirectory(stage, output string) error {
	if _, err := os.Stat(output); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(stage, output); err != nil {
			return fmt.Errorf("publish generated directory: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect output directory: %w", err)
	}

	backup, err := os.MkdirTemp(filepath.Dir(output), "."+filepath.Base(output)+".old-")
	if err != nil {
		return fmt.Errorf("reserve backup path: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("prepare backup path: %w", err)
	}
	if err := os.Rename(output, backup); err != nil {
		return fmt.Errorf("back up existing output: %w", err)
	}
	if err := os.Rename(stage, output); err != nil {
		_ = os.Rename(backup, output)
		return fmt.Errorf("publish generated directory: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove old generated directory %s: %w", backup, err)
	}
	return nil
}
