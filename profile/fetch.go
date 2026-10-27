// Package profile imports client subscriptions into native Xray configurations.
package profile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dalugm/veer/download"
	"github.com/dalugm/veer/settings"
	"github.com/dalugm/veer/sharelink"
)

// Client downloads subscriptions only when the caller explicitly requests a sync.
// HTTP may be supplied for tests; nil uses a bounded, environment-proxy-aware client.
type Client struct{ HTTP *http.Client }

func (c Client) fetch(
	ctx context.Context,
	rawURL string,
	report func(download.Progress),
) ([]sharelink.Node, error) {
	if err := settings.ValidateSubscriptionURL(rawURL); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := http.Client{Timeout: 30 * time.Second}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	// The caller's transport is reusable, but redirects must not downgrade HTTPS.
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if err := settings.ValidateSubscriptionURL(req.URL.String()); err != nil {
			return err
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("HTTPS downgrade refused")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("invalid subscription request")
	}
	req.Header.Set("User-Agent", "Veer")
	req.Header.Set("Accept", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("subscription download failed; check the URL and network")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subscription server returned HTTP %d", resp.StatusCode)
	}
	const limit = 4 << 20
	if resp.ContentLength > limit {
		return nil, errors.New("subscription exceeds 4 MiB")
	}
	reader := download.Track(
		io.LimitReader(resp.Body, limit+1),
		"Subscription",
		resp.ContentLength,
		report,
	)
	data, err := io.ReadAll(reader)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot read subscription response")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return sharelink.DecodeSubscription(data)
}
