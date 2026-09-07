// Package engine translates engine-specific configuration into process plans.
package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Options specifies the executable, native configuration and optional network settings.
type Options struct {
	Binary, Config, GeoDir string
	DNS                    []string
	NetworkService         string
}

// Info describes the privileges and local listeners requested by a configuration.
type Info struct {
	TUN       bool
	TUNName   string
	Endpoints []string
	Protocols []string
}

// Plan contains the commands and environment needed to validate and start a core.
type Plan struct {
	StatsAddress         string
	Binary               string
	Args, CheckArgs, Env []string
	Dir                  string
	Info                 Info
}

// Xray prepares sessions backed by an external Xray executable.
type Xray struct{}

// Prepare validates options and builds the native Xray command plan.
func (Xray) Prepare(o Options) (Plan, error) {
	info, err := Inspect(o.Config)
	if err != nil {
		return Plan{}, err
	}
	path, err := exec.LookPath(o.Binary)
	if err != nil {
		return Plan{}, fmt.Errorf("find Xray: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return Plan{}, err
	}
	config, err := filepath.Abs(o.Config)
	if err != nil {
		return Plan{}, err
	}
	assets := o.GeoDir
	if assets == "" {
		assets = filepath.Dir(config)
	}
	assets, err = filepath.Abs(assets)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Binary:    path,
		Args:      []string{"run", "-c", config},
		CheckArgs: []string{"run", "-test", "-c", config},
		Env:       []string{"XRAY_LOCATION_ASSET=" + assets},
		Dir:       filepath.Dir(config),
		Info:      info,
	}, nil
}

// Inspect reads a native Xray JSON file and identifies its inbound requirements.
func Inspect(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
	if err != nil {
		return Info{}, err
	}
	if len(data) > 8<<20 {
		return Info{}, errors.New("config exceeds 8 MiB")
	}
	var doc struct {
		Inbounds []struct {
			Protocol string `json:"protocol"`
			Settings struct {
				Name string `json:"name"`
			} `json:"settings"`
			Listen string          `json:"listen"`
			Port   json.RawMessage `json:"port"`
		} `json:"inbounds"`
		Outbounds []struct {
			Protocol string `json:"protocol"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Info{}, fmt.Errorf("invalid Xray JSON: %w", err)
	}
	if len(doc.Inbounds) == 0 || len(doc.Outbounds) == 0 {
		return Info{}, errors.New("Xray config needs inbounds and outbounds")
	}
	info := Info{}
	for _, in := range doc.Inbounds {
		if in.Protocol == "tun" {
			info.TUN = true
			info.TUNName = in.Settings.Name
			continue
		}
		if in.Protocol != "socks" && in.Protocol != "http" && in.Protocol != "mixed" {
			continue
		}
		var port int
		if err := json.Unmarshal(in.Port, &port); err != nil {
			var s string
			if json.Unmarshal(in.Port, &s) == nil {
				port, _ = strconv.Atoi(s)
			}
		}
		if port < 1 || port > 65535 {
			continue
		}
		host := in.Listen
		if host == "" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		if host == "::" {
			host = "::1"
		}
		if strings.HasPrefix(host, "/") {
			continue
		}
		info.Endpoints = append(info.Endpoints, net.JoinHostPort(host, strconv.Itoa(port)))
	}
	for _, out := range doc.Outbounds {
		info.Protocols = append(info.Protocols, out.Protocol)
	}
	return info, nil
}
