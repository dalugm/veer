package sharelink

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Node is a named outbound decoded from a share link.
type Node struct {
	Name     string
	Outbound Outbound
}

// DecodeSubscription accepts plain or Base64-encoded VLESS link lists.
// Errors deliberately omit links, credentials and response text.
func DecodeSubscription(data []byte) ([]Node, error) {
	if len(data) > 4<<20 {
		return nil, errors.New("subscription exceeds 4 MiB")
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if !strings.Contains(text, "://") {
		compact := strings.Join(strings.Fields(text), "")
		var decoded []byte
		var err error
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			decoded, err = encoding.DecodeString(compact)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, errors.New("subscription is not a plain or Base64 VLESS list")
		}
		text = strings.TrimSpace(string(decoded))
	}
	var nodes []Node
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		node, err := DecodeVLESSLink(line)
		if err != nil {
			return nil, fmt.Errorf("subscription node %d: %w", len(nodes)+1, err)
		}
		if seen[node.Name] {
			return nil, errors.New("subscription node names must be unique")
		}
		seen[node.Name] = true
		nodes = append(nodes, node)
		if len(nodes) > 1000 {
			return nil, errors.New("subscription exceeds 1000 nodes")
		}
	}
	if len(nodes) == 0 {
		return nil, errors.New("subscription contains no VLESS nodes")
	}
	return nodes, nil
}

// DecodeVLESSLink decodes a VLESS node for client subscription imports.
func DecodeVLESSLink(link string) (Node, error) {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "vless" || u.User == nil || u.Hostname() == "" ||
		(u.Path != "" && u.Path != "/") {
		return Node{}, errors.New("expected a VLESS share link")
	}
	if _, password := u.User.Password(); password {
		return Node{}, errors.New("invalid VLESS user")
	}
	id := u.User.Username()
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return Node{}, errors.New("VLESS user must be a UUID")
	}
	if b, err := hex.DecodeString(strings.ReplaceAll(id, "-", "")); err != nil || len(b) != 16 {
		return Node{}, errors.New("VLESS user must be a UUID")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return Node{}, errors.New("invalid VLESS port")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Node{}, errors.New("invalid VLESS parameters")
	}
	for key, values := range q {
		if len(values) != 1 {
			return Node{}, errors.New("duplicate VLESS parameter")
		}
		switch key {
		case "encryption",
			"flow",
			"type",
			"security",
			"sni",
			"fp",
			"pbk",
			"sid",
			"spx",
			"alpn",
			"allowInsecure",
			"path",
			"host",
			"serviceName",
			"mode",
			"extra":
		case "headerType":
			if values[0] != "none" {
				return Node{}, errors.New("unsupported VLESS header type")
			}
		default:
			return Node{}, errors.New("unsupported VLESS parameter")
		}
	}
	encryption := q.Get("encryption")
	if encryption == "" {
		encryption = "none"
	}
	if encryption != "none" {
		return Node{}, errors.New("VLESS encryption currently supports none only")
	}
	flow := q.Get("flow")
	if flow != "" && flow != "xtls-rprx-vision" {
		return Node{}, errors.New("unsupported VLESS flow")
	}
	stream := &StreamSettings{Network: q.Get("type"), Security: q.Get("security")}
	if stream.Network == "" {
		stream.Network = "tcp"
	}
	if stream.Security == "" {
		stream.Security = "none"
	}
	switch stream.Security {
	case "reality":
		key, err := base64.RawURLEncoding.DecodeString(q.Get("pbk"))
		if err != nil || len(key) != 32 || q.Get("sni") == "" {
			return Node{}, errors.New("REALITY requires a valid public key and SNI")
		}
		sid, err := hex.DecodeString(q.Get("sid"))
		if err != nil || len(sid) > 8 {
			return Node{}, errors.New("invalid REALITY short ID")
		}
		fp := q.Get("fp")
		if fp == "" {
			fp = "chrome"
		}
		stream.RealitySettings = &RealitySettings{
			ServerName:  q.Get("sni"),
			PublicKey:   q.Get("pbk"),
			ShortID:     q.Get("sid"),
			SpiderX:     q.Get("spx"),
			Fingerprint: fp,
		}
	case "tls":
		insecure := false
		if q.Get("allowInsecure") != "" {
			insecure, err = strconv.ParseBool(q.Get("allowInsecure"))
			if err != nil {
				return Node{}, errors.New("invalid TLS allowInsecure value")
			}
		}
		tls := &TLSSettings{
			ServerName:    q.Get("sni"),
			Fingerprint:   q.Get("fp"),
			AllowInsecure: insecure,
		}
		if q.Get("alpn") != "" {
			tls.ALPN = strings.Split(q.Get("alpn"), ",")
		}
		stream.TLSSettings = tls
	case "none":
	default:
		return Node{}, errors.New("unsupported VLESS security")
	}
	switch stream.Network {
	case "tcp", "raw":
	case "ws":
		if stream.Security == "reality" {
			return Node{}, errors.New("REALITY does not support WebSocket")
		}
		stream.WSSettings = &WSSettings{Host: q.Get("host"), Path: q.Get("path")}
	case "grpc":
		stream.GRPCSettings = &GRPCSettings{ServiceName: q.Get("serviceName")}
	case "xhttp":
		stream.XHTTPSettings = &XHTTPSettings{
			Host: q.Get("host"),
			Path: q.Get("path"),
			Mode: q.Get("mode"),
		}
		if q.Get("extra") != "" {
			return Node{}, errors.New("subscription XHTTP extra settings are not supported")
		}
	default:
		return Node{}, errors.New("unsupported VLESS transport")
	}
	name := strings.TrimSpace(u.Fragment)
	if name == "" {
		name = u.Host
	}
	return Node{
		Name: name,
		Outbound: Outbound{
			Protocol: "vless",
			Settings: Settings{
				Vnext: []Vnext{
					{
						Address: u.Hostname(),
						Port:    port,
						Users:   []User{{ID: id, Encryption: encryption, Flow: flow}},
					},
				},
			},
			StreamSettings: stream,
		},
	}, nil
}
