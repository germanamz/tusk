package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const (
	// LatestVersion is the sentinel a user passes (or defaults to) for the
	// newest published release.
	LatestVersion = "latest"

	// DefaultAPIBase is the GitHub API root for the tusk repository.
	// Overridable on Updater so tests can point at an httptest server.
	DefaultAPIBase = "https://api.github.com/repos/germanamz/tusk"

	// ChecksumsAsset is the release asset enumerating every archive's
	// SHA-256, produced by goreleaser's checksum stanza.
	ChecksumsAsset = "checksums.txt"

	resolveTimeout = 30 * time.Second
	resolveSizeCap = 4 << 20 // 4 MiB — release payloads list every asset.
	redirectCap    = 5
	userAgent      = "tusk-selfupdate"
)

// Release is a published release reduced to what an update needs: its tag
// and a name-indexed map of downloadable assets.
type Release struct {
	Version string
	Assets  map[string]string
}

// releasePayload mirrors the subset of GitHub's release JSON we consume.
type releasePayload struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// ArchiveName returns the release archive filename for an OS/architecture
// pair, matching goreleaser's name_template. Windows ships zip; everything
// else ships tar.gz.
func ArchiveName(version string, goos string, goarch string) string {
	bare := strings.TrimPrefix(version, "v")

	if goos == "windows" {
		return fmt.Sprintf("tusk_%s_%s_%s.zip", bare, goos, goarch)
	}

	return fmt.Sprintf("tusk_%s_%s_%s.tar.gz", bare, goos, goarch)
}

// Resolve looks up a release by version. The literal "latest" resolves via
// the releases/latest endpoint; any other value is treated as a tag.
func (updater *Updater) Resolve(ctx context.Context, version string) (Release, error) {
	normalized := NormalizeVersion(version)

	endpoint := updater.apiBase() + "/releases/latest"

	if normalized != LatestVersion {
		// The version is user-supplied and is about to become part of a URL
		// path. Validate before it can introduce separators or dot segments
		// that would repoint resolution at another repository, and escape
		// what survives so it cannot alter the path structure either way.
		if validateErr := ValidateTag(normalized); validateErr != nil {
			return Release{}, validateErr
		}

		endpoint = updater.apiBase() + "/releases/tags/" + url.PathEscape(normalized)
	}

	body, fetchErr := updater.getJSON(ctx, endpoint)

	if fetchErr != nil {
		return Release{}, fetchErr
	}

	var payload releasePayload

	if decodeErr := json.Unmarshal(body, &payload); decodeErr != nil {
		return Release{}, fmt.Errorf("%w: decoding release response: %w", ErrNetwork, decodeErr)
	}

	if payload.TagName == "" {
		return Release{}, fmt.Errorf("%w: release response has no tag_name", ErrNetwork)
	}

	// The tag is server-supplied and flows into the downloaded archive's
	// filename. Validate it here so a hostile or corrupted release response
	// cannot steer a write outside the working directory.
	if validateErr := ValidateTag(payload.TagName); validateErr != nil {
		return Release{}, fmt.Errorf("%w: release reported an unusable tag: %w", ErrNetwork, validateErr)
	}

	assets := make(map[string]string, len(payload.Assets))

	for _, asset := range payload.Assets {
		assets[asset.Name] = asset.URL
	}

	return Release{Version: payload.TagName, Assets: assets}, nil
}

// AssetURL returns the download URL for the archive matching this build's
// OS and architecture, plus the URL of the checksums file that covers it.
func (release Release) AssetURL(goos string, goarch string) (string, string, error) {
	archive := ArchiveName(release.Version, goos, goarch)

	archiveURL, hasArchive := release.Assets[archive]

	if !hasArchive {
		return "", "", fmt.Errorf("%w: release %s ships no archive for %s/%s (%s)",
			ErrNoAsset, release.Version, goos, goarch, archive)
	}

	checksumURL, hasChecksums := release.Assets[ChecksumsAsset]

	if !hasChecksums {
		return "", "", fmt.Errorf("%w: release %s has no %s", ErrNoAsset, release.Version, ChecksumsAsset)
	}

	return archiveURL, checksumURL, nil
}

// HostArchiveName is the archive this build would download, useful for
// messages and dry-run output.
func HostArchiveName(version string) string {
	return ArchiveName(version, runtime.GOOS, runtime.GOARCH)
}

// getJSON performs a size-capped, timeout-bounded GET and returns the body.
func (updater *Updater) getJSON(ctx context.Context, endpoint string) ([]byte, error) {
	response, requestErr := updater.do(ctx, endpoint, resolveTimeout)

	if requestErr != nil {
		return nil, requestErr
	}

	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: no such release (HTTP 404 from %s)", ErrNetwork, endpoint)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d from %s", ErrNetwork, response.StatusCode, endpoint)
	}

	body, readErr := io.ReadAll(io.LimitReader(response.Body, resolveSizeCap))

	if readErr != nil {
		return nil, fmt.Errorf("%w: reading response from %s: %w", ErrNetwork, endpoint, readErr)
	}

	return body, nil
}

// do issues a GET with the shared client policy: bounded timeout, capped
// redirects, and an identifying user agent.
func (updater *Updater) do(ctx context.Context, endpoint string, timeout time.Duration) (*http.Response, error) {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)

	request, requestErr := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)

	if requestErr != nil {
		cancel()

		return nil, fmt.Errorf("%w: building request for %s: %w", ErrNetwork, endpoint, requestErr)
	}

	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= redirectCap {
				return fmt.Errorf("redirect loop after %d hops", redirectCap)
			}

			// Release downloads redirect to a CDN host, which is expected,
			// but a hop must never leave TLS: plaintext would let a network
			// attacker serve both the archive and the checksums that
			// "verify" it, defeating the integrity check entirely.
			if request.URL.Scheme != "https" {
				return fmt.Errorf("refusing redirect to non-HTTPS URL %s", request.URL.Redacted())
			}

			return nil
		},
	}

	response, doErr := client.Do(request)

	if doErr != nil {
		cancel()

		return nil, fmt.Errorf("%w: fetching %s: %w", ErrNetwork, endpoint, doErr)
	}

	// The context must outlive the response body, so cancellation is
	// deferred onto Close rather than fired here.
	response.Body = &cancelOnClose{ReadCloser: response.Body, cancel: cancel}

	return response, nil
}

// cancelOnClose releases a request context when the caller finishes with
// the body, keeping the timeout live for the whole read.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (wrapper *cancelOnClose) Close() error {
	closeErr := wrapper.ReadCloser.Close()

	wrapper.cancel()

	return closeErr
}
