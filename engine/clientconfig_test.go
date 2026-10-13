package engine

import (
	"strings"
	"testing"
)

func TestClientGenerationRejectsLuaTemplates(t *testing.T) {
	for _, section := range []string{"dns", "routing"} {
		data := []byte(`{"` + section + `":{"script":"private-script.lua"}}`)
		proxy, tun, err := BuildClientConfigs(data, "proxy", "short", "id")
		if err == nil || !strings.Contains(err.Error(), "templates with Lua") ||
			strings.Contains(err.Error(), "private-script") || proxy != nil || tun != nil {
			t.Fatalf("generation: %v", err)
		}
	}
}
