// Package subscription generates VLESS links and client configurations from server data.
package subscription

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"uuid"
)

// AddUser creates a client config and adds the corresponding VLESS client to
// the server config. The server config is the source of truth; no manifest is
// required.
func AddUser(serverPath, templatePath, name, outputDir string, stdout io.Writer) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("user name must not be empty")
	}
	if strings.TrimSpace(outputDir) == "" || outputDir == "." {
		return errors.New("output directory must be explicitly specified")
	}
	serverData, err := os.ReadFile(serverPath)
	if err != nil {
		return fmt.Errorf("read server config: %w", err)
	}
	server, err := parseJSONObject(serverData, "server config")
	if err != nil {
		return err
	}
	inbound, reality, err := serverVLESSInbound(server)
	if err != nil {
		return err
	}
	shortID, err := serverShortID(reality)
	if err != nil {
		return err
	}
	for _, value := range inbound["settings"].(map[string]any)["clients"].([]any) {
		client, _ := value.(map[string]any)
		if client != nil && client["email"] == name {
			return fmt.Errorf("user %q already exists", name)
		}
	}

	id := uuid.NewV4().String()
	clients := inbound["settings"].(map[string]any)["clients"].([]any)
	clients = append(clients, map[string]any{"id": id, "flow": "xtls-rprx-vision", "email": name})
	inbound["settings"].(map[string]any)["clients"] = clients

	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read client template: %w", err)
	}
	client, err := parseJSONObject(templateData, "client template")
	if err != nil {
		return err
	}
	if err := applyClientCredentials(client, defaultOutboundTag, id, shortID); err != nil {
		return err
	}
	clientData, err := marshalJSONObject(client)
	if err != nil {
		return err
	}
	proxyDocument, err := parseJSONObject(clientData, "generated TUN client config")
	if err != nil {
		return err
	}
	if err := stripTUN(proxyDocument); err != nil {
		return err
	}
	proxyData, err := marshalJSONObject(proxyDocument)
	if err != nil {
		return err
	}
	config, err := parseXrayConfig(proxyData, "generated proxy client config")
	if err != nil {
		return err
	}
	link, err := config.ToVLESSLink(defaultOutboundTag, name)
	if err != nil {
		return err
	}
	updatedServerData, err := marshalJSONObject(server)
	if err != nil {
		return err
	}
	if err := writeAtomic(
		filepath.Join(outputDir, "config-tun.json"),
		clientData,
		0o600,
	); err != nil {
		return fmt.Errorf("write TUN client config: %w", err)
	}
	if err := writeAtomic(
		filepath.Join(outputDir, "config-proxy.json"),
		proxyData,
		0o600,
	); err != nil {
		return fmt.Errorf("write proxy client config: %w", err)
	}
	if err := writeAtomic(
		filepath.Join(outputDir, "link.txt"),
		[]byte(link+"\n"),
		0o600,
	); err != nil {
		return fmt.Errorf("write VLESS link: %w", err)
	}
	backup := serverPath + ".bak"
	if err := writeAtomic(backup, serverData, 0o600); err != nil {
		return fmt.Errorf("backup server config: %w", err)
	}
	if err := writeAtomic(serverPath, updatedServerData, 0o600); err != nil {
		return fmt.Errorf("write server config: %w", err)
	}
	_, err = fmt.Fprintf(
		stdout,
		"created configs for %s in %s (id %s); server backup: %s\n",
		name,
		outputDir,
		id,
		backup,
	)
	return err
}

func serverVLESSInbound(server map[string]any) (map[string]any, map[string]any, error) {
	inbounds, ok := server["inbounds"].([]any)
	if !ok {
		return nil, nil, errors.New("server config inbounds must be an array")
	}
	for _, value := range inbounds {
		inbound, _ := value.(map[string]any)
		if inbound == nil || inbound["protocol"] != "vless" {
			continue
		}
		settings, _ := inbound["settings"].(map[string]any)
		clients, _ := settings["clients"].([]any)
		stream, _ := inbound["streamSettings"].(map[string]any)
		reality, _ := stream["realitySettings"].(map[string]any)
		if settings == nil || clients == nil || reality == nil {
			return nil, nil, errors.New("VLESS inbound lacks clients or REALITY settings")
		}
		return inbound, reality, nil
	}
	return nil, nil, errors.New("server config has no VLESS inbound")
}

func serverShortID(reality map[string]any) (string, error) {
	shortIDs, ok := reality["shortIds"].([]any)
	if !ok || len(shortIDs) != 1 {
		return "", errors.New("server REALITY must contain exactly one shortId")
	}
	shortID, _ := shortIDs[0].(string)
	if err := validateShortID(shortID); err != nil {
		return "", fmt.Errorf("server reality shortId: %w", err)
	}
	return shortID, nil
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
