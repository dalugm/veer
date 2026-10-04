package engine

import (
	"encoding/hex"
	"errors"
	"fmt"
)

// AddVLESSUser updates a REALITY server document and returns its client short ID.
// The caller owns persistence and backup of the original document.
func AddVLESSUser(data []byte, name, id string) ([]byte, string, error) {
	server, err := parseJSONObject(data, "server config")
	if err != nil {
		return nil, "", err
	}
	inbound, reality, err := serverVLESSInbound(server)
	if err != nil {
		return nil, "", err
	}
	shortID, err := serverShortID(reality)
	if err != nil {
		return nil, "", err
	}
	settings := inbound["settings"].(map[string]any)
	clients := settings["clients"].([]any)
	for _, value := range clients {
		client, _ := value.(map[string]any)
		if client != nil && client["email"] == name {
			return nil, "", fmt.Errorf("user %q already exists", name)
		}
	}
	settings["clients"] = append(
		clients,
		map[string]any{"id": id, "flow": "xtls-rprx-vision", "email": name},
	)
	updated, err := marshalJSONObject(server)
	return updated, shortID, err
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
	if err := ValidateRealityShortID(shortID); err != nil {
		return "", fmt.Errorf("server reality shortId: %w", err)
	}
	return shortID, nil
}

// ValidateRealityShortID checks the supported REALITY short ID encoding.
func ValidateRealityShortID(shortID string) error {
	if len(shortID) < 2 || len(shortID) > 16 || len(shortID)%2 != 0 {
		return errors.New("must be 2 to 16 lowercase hexadecimal characters with an even length")
	}
	decoded, err := hex.DecodeString(shortID)
	if err != nil || hex.EncodeToString(decoded) != shortID {
		return errors.New("must be 2 to 16 lowercase hexadecimal characters with an even length")
	}
	return nil
}
