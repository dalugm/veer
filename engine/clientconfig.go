package engine

import (
	"errors"
	"fmt"
)

// BuildClientConfigs applies VLESS credentials and creates proxy and TUN variants.
func BuildClientConfigs(
	templateData []byte,
	outboundTag, shortID string,
	id string,
) ([]byte, []byte, error) {
	tunDocument, err := parseJSONObject(templateData, "client template")
	if err != nil {
		return nil, nil, err
	}
	if err := applyClientCredentials(tunDocument, outboundTag, id, shortID); err != nil {
		return nil, nil, err
	}
	tunData, err := marshalJSONObject(tunDocument)
	if err != nil {
		return nil, nil, fmt.Errorf("encode TUN client config: %w", err)
	}

	proxyDocument, err := parseJSONObject(tunData, "generated TUN client config")
	if err != nil {
		return nil, nil, err
	}
	if err := stripTUN(proxyDocument); err != nil {
		return nil, nil, err
	}
	proxyData, err := marshalJSONObject(proxyDocument)
	if err != nil {
		return nil, nil, fmt.Errorf("encode proxy client config: %w", err)
	}
	return proxyData, tunData, nil
}

func applyClientCredentials(document map[string]any, outboundTag, id, shortID string) error {
	outbound, err := findTaggedObject(document, "outbounds", outboundTag)
	if err != nil {
		return err
	}
	if protocol, _ := outbound["protocol"].(string); protocol != "vless" {
		return fmt.Errorf("outbound %q must use the VLESS protocol", outboundTag)
	}
	settings, err := requiredObject(outbound, "settings", "VLESS outbound")
	if err != nil {
		return err
	}
	vnext, err := requiredFirstObject(settings, "vnext", "VLESS outbound settings")
	if err != nil {
		return err
	}
	client, err := requiredFirstObject(vnext, "users", "VLESS vnext entry")
	if err != nil {
		return err
	}
	client["id"] = id

	streamSettings, err := requiredObject(outbound, "streamSettings", "VLESS outbound")
	if err != nil {
		return err
	}
	realitySettings, err := requiredObject(
		streamSettings,
		"realitySettings",
		"VLESS streamSettings",
	)
	if err != nil {
		return err
	}
	realitySettings["shortId"] = shortID
	return nil
}

func stripTUN(document map[string]any) error {
	inbounds, err := requiredArray(document, "inbounds", "client template")
	if err != nil {
		return err
	}
	filteredInbounds, removed := removeTaggedObjects(inbounds, "in-tun")
	if !removed {
		return errors.New("client template has no inbound tagged \"in-tun\"")
	}
	document["inbounds"] = filteredInbounds

	outbounds, err := requiredArray(document, "outbounds", "client template")
	if err != nil {
		return err
	}
	filteredOutbounds, removed := removeTaggedObjects(outbounds, "out-dns")
	if !removed {
		return errors.New("client template has no outbound tagged \"out-dns\"")
	}
	document["outbounds"] = filteredOutbounds

	routing, err := requiredObject(document, "routing", "client template")
	if err != nil {
		return err
	}
	rules, err := requiredArray(routing, "rules", "client template routing")
	if err != nil {
		return err
	}
	filteredRules := make([]any, 0, len(rules))
	removedRule := false
	for _, value := range rules {
		rule, ok := value.(map[string]any)
		if !ok {
			return errors.New("client template routing.rules entries must be objects")
		}
		if containsString(rule["inboundTag"], "in-tun") {
			removedRule = true
			continue
		}
		filteredRules = append(filteredRules, rule)
	}
	if !removedRule {
		return errors.New("client template has no routing rule for inbound \"in-tun\"")
	}
	routing["rules"] = filteredRules
	return nil
}
