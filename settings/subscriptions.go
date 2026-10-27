package settings

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

// Subscription stores an explicitly imported feed and its client configuration choice.
// URL is a bearer credential and must never be displayed in views or errors.
type Subscription struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Mode        string `json:"mode,omitempty"`
	Template    string `json:"template,omitempty"`
	OutboundTag string `json:"outbound_tag,omitempty"`
}

// ConfigMode returns the chosen preset or an existing native configuration.
func (s Subscription) ConfigMode() string {
	if s.Mode != "" {
		return s.Mode
	}
	if s.Template != "" {
		return "custom"
	}
	return "mixed"
}

// ValidateConfig checks configuration references without reading files.
func (s Subscription) ValidateConfig() error {
	switch s.ConfigMode() {
	case "mixed", "tun":
		if s.Template != "" || s.OutboundTag != "" {
			return errors.New(
				"default configurations cannot specify a custom configuration or outbound tag",
			)
		}
	case "custom":
		if !filepath.IsAbs(s.Template) {
			return errors.New("choose an existing Xray JSON configuration")
		}
	default:
		return errors.New("choose Mixed, TUN or an existing Xray JSON configuration")
	}
	return nil
}

// Subscription finds the source associated with a profile.
func (c Config) Subscription(id string) (Subscription, bool) {
	for _, source := range c.Subscriptions {
		if source.ID == id {
			return source, true
		}
	}
	return Subscription{}, false
}

// ValidateSubscriptionURL checks a feed URL without echoing its secret path.
func ValidateSubscriptionURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" ||
		u.User != nil ||
		u.Fragment != "" {
		return errors.New("enter an HTTP(S) subscription URL without userinfo or a fragment")
	}
	return nil
}

func (c Config) validateSubscriptions() error {
	ids := make(map[string]bool)
	urls := make(map[string]bool)
	for _, source := range c.Subscriptions {
		if source.ID == "" || ids[source.ID] || strings.TrimSpace(source.Name) == "" ||
			urls[source.URL] {
			return errors.New("invalid or duplicate subscription in settings")
		}
		if err := ValidateSubscriptionURL(source.URL); err != nil {
			return err
		}
		if err := source.ValidateConfig(); err != nil {
			return err
		}
		ids[source.ID], urls[source.URL] = true, true
	}
	keys := make(map[string]bool)
	for _, p := range c.Profiles {
		if p.SubscriptionID == "" {
			if p.NodeKey != "" {
				return errors.New("profile node has no subscription")
			}
			continue
		}
		key := p.SubscriptionID + "/" + p.NodeKey
		if !ids[p.SubscriptionID] || p.NodeKey == "" || keys[key] {
			return errors.New("invalid subscription profile in settings")
		}
		keys[key] = true
	}
	return nil
}
