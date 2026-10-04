package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
)

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
