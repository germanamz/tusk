package typepacks

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	fetchTimeout     = 30 * time.Second
	fetchSizeCap     = 1 << 20 // 1 MiB
	fetchRedirectCap = 3
)

// Fetch reads pack bytes from rawURL. Supports http://, https://, and
// file:// schemes. Enforces a 30s timeout, a 1 MiB size cap, and a
// 3-hop redirect cap.
func Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, parseErr := url.Parse(rawURL)

	if parseErr != nil {
		return nil, fmt.Errorf("pack add: fetch %s: parse url: %w", rawURL, parseErr)
	}

	switch parsed.Scheme {
	case "file":
		body, readErr := os.ReadFile(parsed.Path)

		if readErr != nil {
			return nil, fmt.Errorf("pack add: fetch %s: %w", rawURL, readErr)
		}

		if len(body) > fetchSizeCap {
			return nil, fmt.Errorf("pack add: fetch %s: response exceeds size cap (%d bytes)", rawURL, fetchSizeCap)
		}

		return body, nil

	case "http", "https":
		return fetchHTTP(ctx, rawURL)

	default:
		return nil, fmt.Errorf("pack add: fetch %s: unsupported scheme %q", rawURL, parsed.Scheme)
	}
}

func fetchHTTP(ctx context.Context, rawURL string) ([]byte, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	request, requestErr := http.NewRequestWithContext(fetchCtx, http.MethodGet, rawURL, nil)

	if requestErr != nil {
		return nil, fmt.Errorf("pack add: fetch %s: build request: %w", rawURL, requestErr)
	}

	request.Header.Set("User-Agent", "tusk/v1")

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= fetchRedirectCap {
				return fmt.Errorf("redirect loop after %d hops", fetchRedirectCap)
			}

			return nil
		},
	}

	response, doErr := client.Do(request)

	if doErr != nil {
		if strings.Contains(doErr.Error(), "redirect") {
			return nil, fmt.Errorf("pack add: fetch %s: redirect loop", rawURL)
		}

		return nil, fmt.Errorf("pack add: fetch %s: %w", rawURL, doErr)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pack add: fetch %s: HTTP %d", rawURL, response.StatusCode)
	}

	limited := io.LimitReader(response.Body, int64(fetchSizeCap)+1)
	body, readErr := io.ReadAll(limited)

	if readErr != nil {
		return nil, fmt.Errorf("pack add: fetch %s: read body: %w", rawURL, readErr)
	}

	if len(body) > fetchSizeCap {
		return nil, fmt.Errorf("pack add: fetch %s: response exceeds size cap (%d bytes)", rawURL, fetchSizeCap)
	}

	return body, nil
}
