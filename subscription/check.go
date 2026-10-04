package subscription

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dalugm/veer/engine"
)

func checkManifestWithXray(ctx context.Context, manifestPath, requestedBinary string) (int, error) {
	manifest, baseDir, err := readManifest(manifestPath)
	if err != nil {
		return 0, err
	}
	if err := validateManifest(manifest, baseDir); err != nil {
		return 0, err
	}
	binary, err := engine.ResolveXrayBinary(requestedBinary, baseDir)
	if err != nil {
		return 0, err
	}
	if err := validateGeneratedConfigsWithXray(
		ctx,
		manifest,
		baseDir,
		binary,
		engine.CheckConfig,
	); err != nil {
		return 0, err
	}
	return len(manifest.Users), nil
}

func validateGeneratedConfigsWithXray(
	ctx context.Context,
	manifest *Manifest,
	baseDir, binary string,
	check func(context.Context, string, string, string) error,
) error {
	templateData, err := os.ReadFile(resolveConfigPath(baseDir, manifest.ClientTemplate))
	if err != nil {
		return fmt.Errorf("read client template: %w", err)
	}
	checkDir, err := os.MkdirTemp("", "xraysub-check-*")
	if err != nil {
		return fmt.Errorf("create Xray check directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(checkDir) }()
	if err := os.Chmod(checkDir, 0o700); err != nil {
		return fmt.Errorf("secure Xray check directory: %w", err)
	}

	for i, user := range manifest.Users {
		proxyData, tunData, _, err := buildClientConfigs(
			templateData,
			manifest.OutboundTag,
			manifest.Reality.ShortID,
			user.ID,
		)
		if err != nil {
			return fmt.Errorf("user %q: %w", user.Name, err)
		}
		configs := []struct {
			kind string
			data []byte
		}{
			{kind: "proxy", data: proxyData},
			{kind: "TUN", data: tunData},
		}
		for _, config := range configs {
			path := filepath.Join(
				checkDir,
				fmt.Sprintf("user-%d-%s.json", i, strings.ToLower(config.kind)),
			)
			if err := os.WriteFile(path, config.data, 0o600); err != nil {
				return fmt.Errorf(
					"write temporary %s config for user %q: %w",
					config.kind,
					user.Name,
					err,
				)
			}
			if err := check(ctx, binary, path, baseDir); err != nil {
				return fmt.Errorf("user %q %s config: %w", user.Name, config.kind, err)
			}
		}
	}
	return nil
}
