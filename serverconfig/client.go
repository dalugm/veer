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

	backup := serverPath + ".bak"
	if err := publishFiles([]pendingFile{
		{path: filepath.Join(outputDir, "config-tun.json"), data: clientData},
		{path: filepath.Join(outputDir, "config-proxy.json"), data: proxyData},
		{path: filepath.Join(outputDir, "link.txt"), data: []byte(link + "\n")},
		{path: backup, data: serverData},
		{path: serverPath, data: updatedServerData, expected: serverData},
	}, os.Rename); err != nil {
		return Result{}, err
	}
	return Result{ID: id, ClientDirectory: outputDir, BackupPath: backup}, nil
}
