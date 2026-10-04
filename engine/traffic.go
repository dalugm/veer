package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Traffic is a timestamped cumulative byte count across Xray outbounds.
type Traffic struct {
	Upload, Download uint64
	At               time.Time
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
