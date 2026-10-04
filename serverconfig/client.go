// Package serverconfig persists server users and their corresponding client files.
package serverconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"uuid"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/sharelink"
)

// Result identifies the generated user's files and the original server backup.
type Result struct{ ID, ClientDirectory, BackupPath string }

// AddUser creates a client config and adds the corresponding VLESS client to
// the server config. The server config is the source of truth; no manifest is
// required.
func AddUser(serverPath, templatePath, name, outputDir string) (Result, error) {
	if strings.TrimSpace(name) == "" {
		return Result{}, errors.New("user name must not be empty")
	}
	if strings.TrimSpace(outputDir) == "" || outputDir == "." {
		return Result{}, errors.New("output directory must be explicitly specified")
	}
	serverData, err := os.ReadFile(serverPath)
	if err != nil {
		return Result{}, fmt.Errorf("read server config: %w", err)
	}
	id := uuid.NewV4().String()
	updatedServerData, shortID, err := engine.AddVLESSUser(serverData, name, id)
	if err != nil {
		return Result{}, err
	}

	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return Result{}, fmt.Errorf("read client template: %w", err)
	}
	proxyData, clientData, err := engine.BuildClientConfigs(
		templateData,
		"out-vless",
		shortID,
		id,
	)
	if err != nil {
		return Result{}, err
	}
	config, err := sharelink.Parse(proxyData)
	if err != nil {
		return Result{}, err
	}
	link, err := config.ToVLESSLink("out-vless", name)
	if err != nil {
		return Result{}, err
	}

	if err := writeAtomic(
		filepath.Join(outputDir, "config-tun.json"),
		clientData,
		0o600,
	); err != nil {
		return Result{}, fmt.Errorf("write TUN client config: %w", err)
	}
	if err := writeAtomic(
		filepath.Join(outputDir, "config-proxy.json"),
		proxyData,
		0o600,
	); err != nil {
		return Result{}, fmt.Errorf("write proxy client config: %w", err)
	}
	if err := writeAtomic(
		filepath.Join(outputDir, "link.txt"),
		[]byte(link+"\n"),
		0o600,
	); err != nil {
		return Result{}, fmt.Errorf("write VLESS link: %w", err)
	}
	backup := serverPath + ".bak"
	if err := writeAtomic(backup, serverData, 0o600); err != nil {
		return Result{}, fmt.Errorf("backup server config: %w", err)
	}
	if err := writeAtomic(serverPath, updatedServerData, 0o600); err != nil {
		return Result{}, fmt.Errorf("write server config: %w", err)
	}
	return Result{ID: id, ClientDirectory: outputDir, BackupPath: backup}, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".veer-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
