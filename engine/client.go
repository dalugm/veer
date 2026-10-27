package engine

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/netip"
)

//go:embed client.json
var clientBase []byte

type clientRule struct {
	Type        string   `json:"type"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	OutboundTag string   `json:"outboundTag"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Port        string   `json:"port,omitempty"`
	Network     string   `json:"network,omitempty"`
}

type clientDNS struct {
	Address      string   `json:"address"`
	Domains      []string `json:"domains,omitempty"`
	ExpectedIPs  []string `json:"expectedIPs,omitempty"`
	Tag          string   `json:"tag"`
	SkipFallback bool     `json:"skipFallback,omitempty"`
	FinalQuery   bool     `json:"finalQuery,omitempty"`
}

const (
	clientDirectDNS = "https://223.5.5.5/dns-query"
	clientRemoteDNS = "https://cloudflare-dns.com/dns-query"
)

// DefaultProfile combines a VLESS node with Veer's Mixed or TUN defaults.
// Routing follows v2rayN's bypass-China preset, with DNS derived from the same
// ordered domain rules. Reference: 2dust/v2rayN@57f17d5939e88c8e643ab49b90869d637765e75a,
// ServiceLib/Sample/custom_routing_white and Services/CoreConfig/V2ray/V2rayDnsService.cs.
// The preset uses IPv4 DNS and node dialing; TUN captures both address families.
func DefaultProfile(node []byte, mode string) ([]byte, error) {
	if mode != "mixed" && mode != "tun" {
		return nil, errors.New("choose Mixed or TUN")
	}
	data, err := ApplyProfileNode(clientBase, node, "proxy")
	if err != nil {
		return nil, err
	}
	doc, err := object(data)
	if err != nil {
		return nil, err
	}
	rules := []clientRule{
		{InboundTag: []string{"dns-direct"}, OutboundTag: "direct"},
		{InboundTag: []string{"dns-proxy"}, OutboundTag: "proxy"},
	}
	if mode == "tun" {
		var inbounds []json.RawMessage
		_ = json.Unmarshal(doc["inbounds"], &inbounds)
		inbounds = append(inbounds, json.RawMessage(`{
			"tag":"tun", "protocol":"tun",
			"settings":{
				"mtu":1500, "gateway":["10.0.0.1/30","fc00::1/64"],
				"dns":["1.1.1.1","8.8.8.8"],
				"autoSystemRoutingTable":["0.0.0.0/0","::/0"],
				"autoOutboundsInterface":"auto", "autoSystemDnsToGateway":true
			},
			"sniffing":{"enabled":true,"destOverride":["http","tls","quic"],"routeOnly":true}
		}`))
		doc["inbounds"], err = json.Marshal(inbounds)
		if err != nil {
			return nil, err
		}
		rules = append(rules, clientRule{
			InboundTag: []string{"tun"}, OutboundTag: "dns", Port: "53", Network: "tcp,udp",
		})
	}
	rules = append(rules,
		clientRule{OutboundTag: "block", Port: "443", Network: "udp"},
		clientRule{OutboundTag: "proxy", Domain: []string{"geosite:google"}},
		clientRule{OutboundTag: "direct", IP: []string{"geoip:private"}},
		clientRule{OutboundTag: "direct", Domain: []string{"geosite:private"}},
		clientRule{OutboundTag: "direct", IP: []string{"223.5.5.5", "223.6.6.6"}},
		clientRule{OutboundTag: "direct", Domain: []string{"geosite:cn"}},
		clientRule{OutboundTag: "direct", IP: []string{"geoip:cn"}},
		clientRule{OutboundTag: "proxy", Network: "tcp,udp"},
	)
	for i := range rules {
		rules[i].Type = "field"
	}
	doc["routing"], err = json.Marshal(struct {
		DomainStrategy string       `json:"domainStrategy"`
		Rules          []clientRule `json:"rules"`
	}{"AsIs", rules})
	if err != nil {
		return nil, err
	}
	servers, err := defaultDNSServers(node, rules)
	if err != nil {
		return nil, err
	}
	doc["dns"], err = json.Marshal(struct {
		QueryStrategy string              `json:"queryStrategy"`
		Hosts         map[string][]string `json:"hosts"`
		Servers       []clientDNS         `json:"servers"`
	}{"UseIPv4", map[string][]string{"cloudflare-dns.com": {"1.1.1.1", "1.0.0.1"}}, servers})
	if err != nil {
		return nil, err
	}
	data, err = json.MarshalIndent(doc, "", "  ")
	return append(data, '\n'), err
}

func defaultDNSServers(node []byte, rules []clientRule) ([]clientDNS, error) {
	var out struct {
		Settings struct {
			Vnext []struct{ Address string } `json:"vnext"`
		} `json:"settings"`
	}
	if json.Unmarshal(node, &out) != nil || len(out.Settings.Vnext) != 1 ||
		out.Settings.Vnext[0].Address == "" {
		return nil, errors.New("subscription node needs one server address")
	}
	var servers []clientDNS
	address := out.Settings.Vnext[0].Address
	if _, err := netip.ParseAddr(address); err != nil {
		// Resolve the proxy itself before any DNS query can depend on that proxy.
		// A foreign node IP must not be filtered by the China DNS expectations.
		servers = append(servers, clientDNS{
			Address: clientDirectDNS, Domains: []string{"full:" + address},
			Tag: "dns-direct", SkipFallback: true, FinalQuery: true,
		})
	}
	for _, rule := range rules {
		if len(rule.Domain) == 0 {
			continue
		}
		server := clientDNS{
			Address:      clientRemoteDNS,
			Domains:      rule.Domain,
			Tag:          "dns-proxy",
			SkipFallback: true,
		}
		if rule.OutboundTag == "direct" {
			server.Address, server.Tag = clientDirectDNS, "dns-direct"
			for _, domain := range rule.Domain {
				if domain == "geosite:cn" {
					server.ExpectedIPs = []string{"geoip:cn"}
				}
			}
		}
		servers = append(servers, server)
	}
	servers = append(servers, clientDNS{Address: clientRemoteDNS, Tag: "dns-proxy"})
	return servers, nil
}
