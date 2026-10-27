package engine

import (
	"encoding/json"
	"errors"
)

// ApplyProfileNode replaces one VLESS connection while retaining local policy,
// routing tags, outbound binding and socket options. Neither input is modified.
func ApplyProfileNode(template, node []byte, tag string) ([]byte, error) {
	doc, err := object(template)
	if err != nil || len(doc) == 0 {
		return nil, errors.New("template must be an Xray JSON object")
	}
	var outbounds []map[string]json.RawMessage
	if json.Unmarshal(doc["outbounds"], &outbounds) != nil {
		return nil, errors.New("template needs outbounds")
	}
	index := -1
	for i, out := range outbounds {
		var protocol, currentTag string
		_ = json.Unmarshal(out["protocol"], &protocol)
		_ = json.Unmarshal(out["tag"], &currentTag)
		if (tag != "" && currentTag == tag) || (tag == "" && protocol == "vless") {
			if index != -1 {
				return nil, errors.New("choose a unique VLESS outbound tag in the template")
			}
			if protocol != "vless" {
				return nil, errors.New("template outbound must use VLESS")
			}
			index = i
		}
	}
	if index == -1 {
		return nil, errors.New("template has no matching VLESS outbound")
	}
	replacement, err := object(node)
	if err != nil {
		return nil, errors.New("invalid subscription outbound")
	}
	var protocol string
	_ = json.Unmarshal(replacement["protocol"], &protocol)
	if protocol != "vless" {
		return nil, errors.New("subscription outbound must use VLESS")
	}
	out := outbounds[index]
	stream, err := object(replacement["streamSettings"])
	if err != nil {
		return nil, errors.New("invalid subscription transport")
	}
	local, err := object(out["streamSettings"])
	if err != nil {
		return nil, errors.New("invalid template transport")
	}
	for _, key := range []string{"sockopt", "finalmask"} {
		if value, ok := local[key]; ok {
			stream[key] = value
		}
	}
	out["protocol"] = replacement["protocol"]
	out["settings"] = replacement["settings"]
	out["streamSettings"], err = json.Marshal(stream)
	if err != nil {
		return nil, err
	}
	doc["outbounds"], err = json.Marshal(outbounds)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	return append(data, '\n'), err
}
