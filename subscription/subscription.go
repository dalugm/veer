package subscription

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultOutboundTag = "out-vless"

// XrayConfig contains the outbound configuration used for link generation.
type XrayConfig struct {
	Outbounds []Outbound `json:"outbounds"`
}

// Outbound describes a tagged proxy outbound.
type Outbound struct {
	Protocol       string          `json:"protocol"`
	Tag            string          `json:"tag"`
	Settings       Settings        `json:"settings"`
	StreamSettings *StreamSettings `json:"streamSettings,omitempty"`
}

// Settings contains the outbound server list.
type Settings struct {
	Vnext []Vnext `json:"vnext"`
}

// Vnext describes a VLESS server and its users.
type Vnext struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Users   []User `json:"users"`
}

// User contains VLESS authentication and flow settings.
type User struct {
	ID         string `json:"id"`
	Encryption string `json:"encryption"`
	Flow       string `json:"flow,omitempty"`
}

// StreamSettings contains transport and security options.
type StreamSettings struct {
	Network         string           `json:"network"`
	Security        string           `json:"security"`
	TLSSettings     *TLSSettings     `json:"tlsSettings,omitempty"`
	RealitySettings *RealitySettings `json:"realitySettings,omitempty"`
	WSSettings      *WSSettings      `json:"wsSettings,omitempty"`
	GRPCSettings    *GRPCSettings    `json:"grpcSettings,omitempty"`
	XHTTPSettings   *XHTTPSettings   `json:"xhttpSettings,omitempty"`
}

// TLSSettings contains TLS handshake options.
type TLSSettings struct {
	ServerName    string   `json:"serverName"`
	AllowInsecure bool     `json:"allowInsecure"`
	Fingerprint   string   `json:"fingerprint"`
	ALPN          []string `json:"alpn"`
}

// RealitySettings contains REALITY handshake options.
type RealitySettings struct {
	ServerName  string `json:"serverName"`
	Password    string `json:"password"`
	PublicKey   string `json:"publicKey"`
	ShortID     string `json:"shortId"`
	SpiderX     string `json:"spiderX"`
	Fingerprint string `json:"fingerprint"`
}

// WSSettings contains WebSocket transport options.
type WSSettings struct {
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
}

// GRPCSettings contains gRPC transport options.
type GRPCSettings struct {
	ServiceName string `json:"serviceName"`
}

// XHTTPSettings contains XHTTP transport options.
type XHTTPSettings struct {
	Host  string          `json:"host"`
	Path  string          `json:"path"`
	Mode  string          `json:"mode"`
	Extra json.RawMessage `json:"extra"`
}

var xrayCommand = exec.Command

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

// Generate validates the manifest and writes one bundle per user.
func Generate(manifestPath, outputDir, xrayBinary string, stdout io.Writer) error {
	if _, err := checkManifestWithXray(manifestPath, xrayBinary); err != nil {
		return err
	}
	count, err := generate(manifestPath, outputDir)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "generated %d subscription bundles in %s\n", count, outputDir)
	return err
}

// Check validates a manifest and all generated client variants.
func Check(manifestPath, xrayBinary string, stdout io.Writer) error {
	count, err := checkManifestWithXray(manifestPath, xrayBinary)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "checked %d subscription users\n", count)
	return err
}

// Link creates a VLESS link from an Xray client config.
func Link(configPath, outboundTag, label string, stdout io.Writer) error {
	_, config, err := readXrayConfig(configPath)
	if err != nil {
		return err
	}
	link, err := config.ToVLESSLink(outboundTag, label)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, link)
	return err
}

// Token generates a random subscription token.
func Token(stdout io.Writer) error {
	token, err := newToken()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, token)
	return err
}

// ToVLESSLink encodes the selected outbound as a VLESS share link.
func (c *XrayConfig) ToVLESSLink(outboundTag, label string) (string, error) {
	var vless *Outbound
	for i := range c.Outbounds {
		o := &c.Outbounds[i]
		if o.Protocol == "vless" && (outboundTag == "" || o.Tag == outboundTag) {
			vless = o
			break
		}
	}
	if vless == nil {
		return "", fmt.Errorf("no VLESS outbound with tag %q", outboundTag)
	}
	if len(vless.Settings.Vnext) != 1 {
		return "", fmt.Errorf("VLESS outbound %q must contain exactly one vnext entry", vless.Tag)
	}

	vnext := vless.Settings.Vnext[0]
	if vnext.Address == "" || vnext.Port < 1 || vnext.Port > 65535 {
		return "", fmt.Errorf("VLESS outbound %q has an invalid server address or port", vless.Tag)
	}
	if len(vnext.Users) != 1 {
		return "", fmt.Errorf("VLESS outbound %q must contain exactly one user", vless.Tag)
	}
	user := vnext.Users[0]
	if user.ID == "" {
		return "", fmt.Errorf("VLESS outbound %q has an empty user id", vless.Tag)
	}

	params := url.Values{}
	if user.Encryption == "" {
		params.Set("encryption", "none")
	} else {
		params.Set("encryption", user.Encryption)
	}

	stream := vless.StreamSettings
	if stream == nil {
		return "", fmt.Errorf("VLESS outbound %q has no streamSettings", vless.Tag)
	}
	if stream.Network == "" {
		return "", fmt.Errorf("VLESS outbound %q has no transport type", vless.Tag)
	}
	params.Set("type", stream.Network)

	switch stream.Security {
	case "reality":
		if stream.RealitySettings == nil {
			return "", errors.New("REALITY security is missing realitySettings")
		}
		r := stream.RealitySettings
		// Xray calls this value publicKey. Password is retained as a
		// backwards-compatible alias for older manifests.
		publicKey := r.PublicKey
		if publicKey == "" {
			publicKey = r.Password
		}
		if publicKey == "" || r.ServerName == "" || r.ShortID == "" {
			return "", errors.New("REALITY requires password/publicKey, serverName, and shortId")
		}
		params.Set("security", "reality")
		params.Set("pbk", publicKey)
		params.Set("sid", r.ShortID)
		params.Set("sni", r.ServerName)
		if r.SpiderX != "" {
			params.Set("spx", r.SpiderX)
		}
		if r.Fingerprint != "" {
			params.Set("fp", r.Fingerprint)
		}
	case "tls":
		params.Set("security", "tls")
		if stream.TLSSettings != nil {
			if stream.TLSSettings.ServerName != "" {
				params.Set("sni", stream.TLSSettings.ServerName)
			}
			if stream.TLSSettings.AllowInsecure {
				params.Set("allowInsecure", "1")
			}
			if stream.TLSSettings.Fingerprint != "" {
				params.Set("fp", stream.TLSSettings.Fingerprint)
			}
			if len(stream.TLSSettings.ALPN) > 0 {
				params.Set("alpn", strings.Join(stream.TLSSettings.ALPN, ","))
			}
		}

	case "", "none":
		params.Set("security", "none")

	default:
		return "", fmt.Errorf("unsupported stream security %q", stream.Security)
	}
	if user.Flow != "" {
		params.Set("flow", user.Flow)
	}

	switch stream.Network {
	case "raw", "tcp":
	case "ws":
		if stream.WSSettings == nil {
			return "", errors.New("WebSocket transport is missing wsSettings")
		}
		if stream.WSSettings.Path != "" {
			params.Set("path", stream.WSSettings.Path)
		}
		if host := stream.WSSettings.Headers["Host"]; host != "" {
			params.Set("host", host)
		}
	case "grpc":
		if stream.GRPCSettings == nil {
			return "", errors.New("gRPC transport is missing grpcSettings")
		}
		if stream.GRPCSettings.ServiceName != "" {
			params.Set("serviceName", stream.GRPCSettings.ServiceName)
		}
	case "xhttp":
		if stream.XHTTPSettings == nil {
			return "", errors.New("XHTTP transport is missing xhttpSettings")
		}
		xhttp := stream.XHTTPSettings
		path := xhttp.Path
		if path == "" {
			path = "/"
		}
		params.Set("path", path)
		if xhttp.Host != "" {
			params.Set("host", xhttp.Host)
		}
		if xhttp.Mode != "" {
			params.Set("mode", xhttp.Mode)
		}
		extra := bytes.TrimSpace(xhttp.Extra)
		if len(extra) > 0 && !bytes.Equal(extra, []byte("null")) {
			var compact bytes.Buffer
			if err := json.Compact(&compact, extra); err != nil {
				return "", fmt.Errorf("XHTTP extra is invalid JSON: %w", err)
			}
			params.Set("extra", compact.String())
		}
	default:
		return "", fmt.Errorf("unsupported transport type %q", stream.Network)
	}

	if strings.TrimSpace(label) == "" {
		return "", errors.New("node label must not be empty")
	}
	u := &url.URL{
		Scheme:   "vless",
		User:     url.User(user.ID),
		Host:     net.JoinHostPort(vnext.Address, strconv.Itoa(vnext.Port)),
		RawQuery: params.Encode(),
		Fragment: label,
	}
	return u.String(), nil
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

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func readXrayConfig(path string) ([]byte, *XrayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var config XrayConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return data, &config, nil
}

func parseXrayConfig(data []byte, source string) (*XrayConfig, error) {
	var config XrayConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	return &config, nil
}

func parseJSONObject(data []byte, source string) (map[string]any, error) {
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	if document == nil {
		return nil, fmt.Errorf("parse %s: top-level value must be an object", source)
	}
	return document, nil
}

func marshalJSONObject(document map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func buildClientConfigs(
	templateData []byte,
	outboundTag, shortID string,
	user ManifestUser,
) ([]byte, []byte, *XrayConfig, error) {
	tunDocument, err := parseJSONObject(templateData, "client template")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := applyClientCredentials(tunDocument, outboundTag, user.ID, shortID); err != nil {
		return nil, nil, nil, err
	}
	tunData, err := marshalJSONObject(tunDocument)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode TUN client config: %w", err)
	}

	proxyDocument, err := parseJSONObject(tunData, "generated TUN client config")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := stripTUN(proxyDocument); err != nil {
		return nil, nil, nil, err
	}
	proxyData, err := marshalJSONObject(proxyDocument)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode proxy client config: %w", err)
	}
	config, err := parseXrayConfig(proxyData, "generated proxy client config")
	if err != nil {
		return nil, nil, nil, err
	}
	return proxyData, tunData, config, nil
}

func applyClientCredentials(document map[string]any, outboundTag, id, shortID string) error {
	outbound, err := findTaggedObject(document, "outbounds", outboundTag)
	if err != nil {
		return err
	}
	if protocol, _ := outbound["protocol"].(string); protocol != "vless" {
		return fmt.Errorf("outbound %q must use the VLESS protocol", outboundTag)
	}
	settings, err := requiredObject(outbound, "settings", "VLESS outbound")
	if err != nil {
		return err
	}
	vnext, err := requiredFirstObject(settings, "vnext", "VLESS outbound settings")
	if err != nil {
		return err
	}
	client, err := requiredFirstObject(vnext, "users", "VLESS vnext entry")
	if err != nil {
		return err
	}
	client["id"] = id

	streamSettings, err := requiredObject(outbound, "streamSettings", "VLESS outbound")
	if err != nil {
		return err
	}
	realitySettings, err := requiredObject(
		streamSettings,
		"realitySettings",
		"VLESS streamSettings",
	)
	if err != nil {
		return err
	}
	realitySettings["shortId"] = shortID
	return nil
}

func stripTUN(document map[string]any) error {
	inbounds, err := requiredArray(document, "inbounds", "client template")
	if err != nil {
		return err
	}
	filteredInbounds, removed := removeTaggedObjects(inbounds, "in-tun")
	if !removed {
		return errors.New("client template has no inbound tagged \"in-tun\"")
	}
	document["inbounds"] = filteredInbounds

	outbounds, err := requiredArray(document, "outbounds", "client template")
	if err != nil {
		return err
	}
	filteredOutbounds, removed := removeTaggedObjects(outbounds, "out-dns")
	if !removed {
		return errors.New("client template has no outbound tagged \"out-dns\"")
	}
	document["outbounds"] = filteredOutbounds

	routing, err := requiredObject(document, "routing", "client template")
	if err != nil {
		return err
	}
	rules, err := requiredArray(routing, "rules", "client template routing")
	if err != nil {
		return err
	}
	filteredRules := make([]any, 0, len(rules))
	removedRule := false
	for _, value := range rules {
		rule, ok := value.(map[string]any)
		if !ok {
			return errors.New("client template routing.rules entries must be objects")
		}
		if containsString(rule["inboundTag"], "in-tun") {
			removedRule = true
			continue
		}
		filteredRules = append(filteredRules, rule)
	}
	if !removedRule {
		return errors.New("client template has no routing rule for inbound \"in-tun\"")
	}
	routing["rules"] = filteredRules
	return nil
}

func findTaggedObject(document map[string]any, field, tag string) (map[string]any, error) {
	items, err := requiredArray(document, field, "client template")
	if err != nil {
		return nil, err
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("client template %s entries must be objects", field)
		}
		if itemTag, _ := item["tag"].(string); itemTag == tag {
			return item, nil
		}
	}
	return nil, fmt.Errorf("client template has no %s entry tagged %q", field, tag)
}

func requiredObject(parent map[string]any, field, context string) (map[string]any, error) {
	object, ok := parent[field].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.%s must be an object", context, field)
	}
	return object, nil
}

func requiredArray(parent map[string]any, field, context string) ([]any, error) {
	array, ok := parent[field].([]any)
	if !ok {
		return nil, fmt.Errorf("%s.%s must be an array", context, field)
	}
	return array, nil
}

func requiredFirstObject(parent map[string]any, field, context string) (map[string]any, error) {
	array, err := requiredArray(parent, field, context)
	if err != nil {
		return nil, err
	}
	if len(array) != 1 {
		return nil, fmt.Errorf("%s.%s must contain exactly one entry", context, field)
	}
	object, ok := array[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.%s entry must be an object", context, field)
	}
	return object, nil
}

func removeTaggedObjects(items []any, tag string) ([]any, bool) {
	filtered := make([]any, 0, len(items))
	removed := false
	for _, value := range items {
		item, ok := value.(map[string]any)
		if ok {
			if itemTag, _ := item["tag"].(string); itemTag == tag {
				removed = true
				continue
			}
		}
		filtered = append(filtered, value)
	}
	return filtered, removed
}

func containsString(value any, target string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if text, ok := item.(string); ok && text == target {
			return true
		}
	}
	return false
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
	if err := validateShortID(manifest.Reality.ShortID); err != nil {
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
			user,
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

func checkManifestWithXray(manifestPath, requestedBinary string) (int, error) {
	manifest, baseDir, err := readManifest(manifestPath)
	if err != nil {
		return 0, err
	}
	if err := validateManifest(manifest, baseDir); err != nil {
		return 0, err
	}
	binary, err := resolveXrayBinary(baseDir, requestedBinary)
	if err != nil {
		return 0, err
	}
	if err := validateGeneratedConfigsWithXray(manifest, baseDir, binary); err != nil {
		return 0, err
	}
	return len(manifest.Users), nil
}

func resolveXrayBinary(baseDir, requested string) (string, error) {
	if requested != "" {
		if strings.ContainsRune(requested, filepath.Separator) || filepath.IsAbs(requested) {
			absolute, err := filepath.Abs(requested)
			if err != nil {
				return "", fmt.Errorf("resolve Xray binary: %w", err)
			}
			if err := validateExecutable(absolute); err != nil {
				return "", err
			}
			return absolute, nil
		}
		binary, err := exec.LookPath(requested)
		if err != nil {
			return "", fmt.Errorf("find Xray binary %q: %w", requested, err)
		}
		return binary, nil
	}

	besideManifest := filepath.Join(baseDir, "xray")
	if err := validateExecutable(besideManifest); err == nil {
		return besideManifest, nil
	}
	binary, err := exec.LookPath("xray")
	if err != nil {
		return "", errors.New(
			"xray binary not found beside the manifest or in PATH; set its executable path",
		)
	}
	return binary, nil
}

func validateExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect Xray binary %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("xray binary is not an executable regular file: %s", path)
	}
	return nil
}

func validateGeneratedConfigsWithXray(manifest *Manifest, baseDir, binary string) error {
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
			user,
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
			if err := runXrayConfigCheck(binary, path, baseDir); err != nil {
				return fmt.Errorf("user %q %s config: %w", user.Name, config.kind, err)
			}
		}
	}
	return nil
}

func runXrayConfigCheck(binary, configPath, assetDir string) error {
	var stderr bytes.Buffer
	command := xrayCommand(binary, "run", "-dump", "-config", configPath)
	command.Dir = assetDir
	env := command.Env
	if env == nil {
		env = os.Environ()
	}
	command.Env = append(env, "XRAY_LOCATION_ASSET="+assetDir)
	command.Stdout = io.Discard
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if len(message) > 2000 {
			message = message[:2000] + "..."
		}
		if message == "" {
			return fmt.Errorf("xray rejected the config: %w", err)
		}
		return fmt.Errorf("xray rejected the config: %w: %s", err, message)
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

func validateShortID(shortID string) error {
	if len(shortID) < 2 || len(shortID) > 16 || len(shortID)%2 != 0 {
		return errors.New("must be 2 to 16 lowercase hexadecimal characters with an even length")
	}
	decoded, err := hex.DecodeString(shortID)
	if err != nil || hex.EncodeToString(decoded) != shortID {
		return errors.New("must be 2 to 16 lowercase hexadecimal characters with an even length")
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

func generate(manifestPath, outputDir string) (int, error) {
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
		proxyData, tunData, config, err := buildClientConfigs(
			templateData,
			manifest.OutboundTag,
			manifest.Reality.ShortID,
			user,
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
