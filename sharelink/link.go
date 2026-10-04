// Package sharelink encodes proxy outbounds as shareable URIs.
package sharelink

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

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

// Parse reads the outbound fields needed to encode a share link.
func Parse(data []byte) (*XrayConfig, error) {
	var config XrayConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse Xray share-link configuration: %w", err)
	}
	return &config, nil
}
