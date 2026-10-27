package engine

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestApplyProfileNodePreservesLocalPolicyAndBindings(t *testing.T) {
	template := []byte(
		`{"inbounds":[{"protocol":"tun","tag":"in-tun"}],"dns":{"servers":["223.5.5.5"]},"routing":{"rules":[{"outboundTag":"proxy","domain":["geosite:geolocation-!cn"]}]},"custom":{"precise":9007199254740993},"outbounds":[{"tag":"proxy","protocol":"vless","sendThrough":"192.0.2.1","settings":{"old":true},"streamSettings":{"method":"ws","security":"tls","wsSettings":{"path":"/old"},"sockopt":{"domainStrategy":"ForceIPv4"}}},{"tag":"direct","protocol":"freedom"}]}`,
	)
	node := []byte(
		`{"protocol":"vless","settings":{"vnext":[{"address":"new.example","port":443,"users":[{"id":"new-user"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"new.example"}}}`,
	)
	got, err := ApplyProfileNode(template, node, "")
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal(template, &before)
	_ = json.Unmarshal(got, &after)
	for _, key := range []string{"inbounds", "dns", "routing", "custom"} {
		var b, a any
		decoder := func(data []byte) any { var value any; _ = json.Unmarshal(data, &value); return value }
		b, a = decoder(before[key]), decoder(after[key])
		if !reflect.DeepEqual(b, a) {
			t.Fatalf("changed %s", key)
		}
	}
	var out []struct {
		Tag            string
		SendThrough    string
		StreamSettings map[string]json.RawMessage
	}
	if err := json.Unmarshal(after["outbounds"], &out); err != nil {
		t.Fatal(err)
	}
	if out[0].Tag != "proxy" || out[0].SendThrough != "192.0.2.1" ||
		len(out[0].StreamSettings["sockopt"]) == 0 ||
		out[0].StreamSettings["method"] != nil ||
		out[0].StreamSettings["wsSettings"] != nil ||
		out[0].StreamSettings["tlsSettings"] != nil {
		t.Fatal("lost binding or retained stale transport")
	}
	var precise map[string]json.RawMessage
	_ = json.Unmarshal(after["custom"], &precise)
	if string(precise["precise"]) != "9007199254740993" {
		t.Fatal("lost numeric precision")
	}
}

func TestApplyProfileNodeRequiresUnambiguousProxy(t *testing.T) {
	data := []byte(`{"outbounds":[{"protocol":"vless","tag":"a"},{"protocol":"vless","tag":"b"}]}`)
	node := []byte(`{"protocol":"vless","settings":{},"streamSettings":{"network":"tcp"}}`)
	if _, err := ApplyProfileNode(data, node, ""); err == nil {
		t.Fatal("silently selected one of multiple outbounds")
	}
	if _, err := ApplyProfileNode(data, node, "b"); err != nil {
		t.Fatal(err)
	}
}
