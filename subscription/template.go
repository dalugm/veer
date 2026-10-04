package subscription

import (
	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/sharelink"
)

func buildClientConfigs(
	template []byte,
	tag, shortID, id string,
) ([]byte, []byte, *sharelink.XrayConfig, error) {
	proxy, tun, err := engine.BuildClientConfigs(template, tag, shortID, id)
	if err != nil {
		return nil, nil, nil, err
	}
	config, err := sharelink.Parse(proxy)
	return proxy, tun, config, err
}
