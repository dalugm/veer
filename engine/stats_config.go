package engine

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
)

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
