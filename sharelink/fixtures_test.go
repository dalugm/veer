package sharelink

func testConfig() *XrayConfig {
	return &XrayConfig{Outbounds: []Outbound{{
		Protocol: "vless",
		Tag:      "out-vless",
		Settings: Settings{Vnext: []Vnext{{
			Address: "203.0.113.10",
			Port:    443,
			Users: []User{{
				ID:         "4521497c-0eac-41f3-8746-1afcbacc205c",
				Encryption: "none",
				Flow:       "xtls-rprx-vision",
			}},
		}}},
		StreamSettings: &StreamSettings{
			Network:  "raw",
			Security: "reality",
			RealitySettings: &RealitySettings{
				ServerName:  "example.com",
				Password:    "public-password",
				ShortID:     "0123456789abcdef",
				Fingerprint: "chrome",
			},
		},
	}}}
}
