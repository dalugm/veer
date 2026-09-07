package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Traffic is a timestamped cumulative byte count across Xray outbounds.
type Traffic struct {
	Upload, Download uint64
	At               time.Time
}

// PrepareRuntime creates a private session config with a loopback statistics API.
// The caller must run cleanup after the core exits, including on startup failure.
func (Xray) PrepareRuntime(plan Plan) (Plan, func() error, error) {
	if len(plan.Args) != 3 || plan.Args[1] != "-c" || len(plan.CheckArgs) != 4 ||
		plan.CheckArgs[2] != "-c" {
		return Plan{}, nil, errors.New("invalid Xray runtime plan")
	}
	f, err := os.Open(plan.Args[2])
	if err != nil {
		return Plan{}, nil, fmt.Errorf("open runtime config: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
	err = errors.Join(err, f.Close())
	if err != nil {
		return Plan{}, nil, err
	}
	if len(data) > 8<<20 {
		return Plan{}, nil, errors.New("config exceeds 8 MiB")
	}
	doc, err := object(data)
	if err != nil {
		return Plan{}, nil, errors.New("cannot prepare Xray statistics configuration")
	}
	api, err := object(doc["api"])
	if err != nil {
		return Plan{}, nil, errors.New("cannot prepare Xray statistics configuration")
	}
	var address string
	if len(api["listen"]) > 0 {
		if err := json.Unmarshal(api["listen"], &address); err != nil {
			return Plan{}, nil, errors.New("cannot prepare Xray statistics configuration")
		}
	}
	if len(api) > 0 && address == "" {
		// Direct listening disables Xray's legacy tagged API outbound.
		// Keep routed API configurations intact and disable traffic sampling.
		plan.StatsAddress = ""
		return plan, func() error { return nil }, nil
	}
	if address != "" {
		if !loopbackStatsAddress(address) {
			// Preserve existing listeners; enabling StatsService on a public API
			// would expose new information and replacing it would break clients.
			plan.StatsAddress = ""
			return plan, func() error { return nil }, nil
		}
	} else {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return Plan{}, nil, fmt.Errorf("allocate statistics address: %w", err)
		}
		address = listener.Addr().String()
		if err := listener.Close(); err != nil {
			return Plan{}, nil, err
		}
	}
	data, err = statsConfig(data, address)
	if err != nil {
		return Plan{}, nil, errors.New("cannot prepare Xray statistics configuration")
	}
	temp, err := os.CreateTemp("", "veer-runtime-*.json")
	if err != nil {
		return Plan{}, nil, fmt.Errorf("create runtime config: %w", err)
	}
	cleanup := func() error {
		err := os.Remove(temp.Name())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("remove runtime config: %w", err)
		}
		return nil
	}
	_, writeErr := temp.Write(data)
	if err := errors.Join(writeErr, temp.Close()); err != nil {
		return Plan{}, nil, errors.Join(err, cleanup())
	}
	plan.Args = slices.Clone(plan.Args)
	plan.CheckArgs = slices.Clone(plan.CheckArgs)
	plan.Args[2] = temp.Name()
	plan.CheckArgs[3] = temp.Name()
	plan.StatsAddress = address
	return plan, cleanup, nil
}

func loopbackStatsAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	number, err := strconv.Atoi(port)
	return err == nil && ip != nil && ip.IsLoopback() && number >= 1 && number <= 65535
}

type rawObject map[string]json.RawMessage

func object(data json.RawMessage) (rawObject, error) {
	value := rawObject{}
	if len(data) == 0 || string(data) == "null" {
		return value, nil
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func statsConfig(data []byte, address string) ([]byte, error) {
	doc, err := object(data)
	if err != nil {
		return nil, err
	}
	api, err := object(doc["api"])
	if err != nil {
		return nil, err
	}
	var services []string
	if len(api["services"]) > 0 {
		if err := json.Unmarshal(api["services"], &services); err != nil {
			return nil, err
		}
	}
	if !slices.Contains(services, "StatsService") {
		services = append(services, "StatsService")
	}
	api["services"], _ = json.Marshal(services)
	api["listen"], _ = json.Marshal(address)
	doc["api"], _ = json.Marshal(api)
	policy, err := object(doc["policy"])
	if err != nil {
		return nil, err
	}
	system, err := object(policy["system"])
	if err != nil {
		return nil, err
	}
	system["statsOutboundUplink"] = json.RawMessage("true")
	system["statsOutboundDownlink"] = json.RawMessage("true")
	policy["system"], _ = json.Marshal(system)
	doc["policy"], _ = json.Marshal(policy)
	if len(doc["stats"]) == 0 || string(doc["stats"]) == "null" {
		doc["stats"] = json.RawMessage("{}")
	}
	used := map[string]bool{}
	var outbounds []rawObject
	if err := json.Unmarshal(doc["outbounds"], &outbounds); err != nil {
		return nil, err
	}
	for _, key := range []string{"inbounds", "outbounds"} {
		var entries []rawObject
		if len(doc[key]) > 0 {
			if err := json.Unmarshal(doc[key], &entries); err != nil {
				return nil, err
			}
		}
		for _, entry := range entries {
			var tag string
			if err := json.Unmarshal(entry["tag"], &tag); len(entry["tag"]) > 0 && err != nil {
				return nil, err
			}
			used[tag] = true
		}
	}
	var apiTag string
	_ = json.Unmarshal(api["tag"], &apiTag)
	if apiTag == "" {
		for next := 1; ; next++ {
			apiTag = "veer-api-" + strconv.Itoa(next)
			if !used[apiTag] {
				break
			}
		}
		api["tag"], _ = json.Marshal(apiTag)
		doc["api"], _ = json.Marshal(api)
	}
	used[apiTag] = true
	next := 1
	for _, out := range outbounds {
		if out == nil {
			return nil, errors.New("null outbound")
		}
		var tag string
		_ = json.Unmarshal(out["tag"], &tag)
		if tag != "" {
			continue
		}
		for {
			tag = "veer-outbound-" + strconv.Itoa(next)
			next++
			if !used[tag] {
				break
			}
		}
		used[tag] = true
		out["tag"], _ = json.Marshal(tag)
	}
	doc["outbounds"], _ = json.Marshal(outbounds)
	return json.Marshal(doc)
}

// SampleTraffic queries the core's loopback API without resetting its counters.
func SampleTraffic(ctx context.Context, binary, address string) (Traffic, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		binary,
		"api",
		"statsquery",
		"--server="+address,
		"-timeout",
		"2",
		"-pattern",
		"outbound>>>",
	)
	hideQueryWindow(cmd)
	output := &boundedOutput{remaining: 1 << 20}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Run(); err != nil {
		return Traffic{}, errors.New("Xray traffic statistics unavailable")
	}
	if output.exceeded {
		return Traffic{}, errors.New("Xray traffic statistics response too large")
	}
	traffic, err := parseTraffic(output.Bytes())
	if err != nil {
		return Traffic{}, errors.New("invalid Xray traffic statistics response")
	}
	traffic.At = time.Now()
	return traffic, nil
}

type boundedOutput struct {
	bytes.Buffer
	remaining int
	exceeded  bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if n > b.remaining {
		b.exceeded = true
		p = p[:b.remaining]
	}
	_, _ = b.Buffer.Write(p)
	b.remaining -= len(p)
	return n, nil
}

func parseTraffic(data []byte) (Traffic, error) {
	var response struct {
		Stat []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return Traffic{}, err
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return Traffic{}, errors.New("null statistics")
	}
	result := Traffic{}
	for _, stat := range response.Stat {
		parts := strings.Split(stat.Name, ">>>")
		if len(parts) != 4 || parts[0] != "outbound" || parts[1] == "" || parts[2] != "traffic" ||
			(parts[3] != "uplink" && parts[3] != "downlink") {
			continue
		}
		number := string(stat.Value)
		if len(number) > 0 && number[0] == '"' {
			if err := json.Unmarshal(stat.Value, &number); err != nil {
				return Traffic{}, err
			}
		}
		if number == "" {
			number = "0"
		} // ProtoJSON may omit zero values.
		value, err := strconv.ParseUint(number, 10, 64)
		if err != nil {
			return Traffic{}, err
		}
		target := &result.Upload
		if parts[3] == "downlink" {
			target = &result.Download
		}
		if value > math.MaxUint64-*target {
			return Traffic{}, errors.New("counter overflow")
		}
		*target += value
	}
	return result, nil
}
