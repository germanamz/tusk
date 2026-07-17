package bookview

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secretBody is the sentinel every out-of-reach fixture file carries. Each
// denial test asserts its own status code, but the suite also asserts this byte
// string never appears in any response body — a status assertion alone would
// not catch a guard that denied with the wrong code while still writing the
// bytes.
const secretBody = "SECRET-MUST-NEVER-BE-SERVED"

// assetVault builds the fixture the asset tests share and returns the vault
// root. The layout is deliberate; every entry exists to make one specific
// bypass reachable:
//
//	<base>/vault/                       the vault root
//	<base>/vault/img/x.png              a plain asset — the happy path
//	<base>/vault/index.html             probes http.ServeFile's own /index.html redirect
//	<base>/vault/sub/                   a directory — must not be listed
//	<base>/vault/inside.png    -> img/x.png            symlink that does NOT escape: allowed
//	<base>/vault/escape.txt    -> <base>/outside/...   symlink escape: denied
//	<base>/vault/sneaky.txt    -> <base>/vault-secrets/...  the prefix trap: denied
//	<base>/vault/innocent.png  -> <base>/vault/.tusk/index.db  dot trap, file: denied
//	<base>/vault/linkdot       -> <base>/vault/.git    dot trap, directory: denied
//	<base>/vault/.tusk/index.db         the index — denied
//	<base>/vault/.git/config            a git config (tokens live here) — denied
//	<base>/vault/.hidden                a dotfile — denied
//	<base>/vault-secrets/creds.txt      sibling whose path has "<base>/vault" as a
//	                                    STRING prefix but is not inside the vault
//	<base>/outside/secret.txt           a plain out-of-vault secret
//
// "<base>/vault-secrets" is the reason base and root are separate: t.TempDir()
// alone cannot produce two directories where one's path is a string prefix of
// the other's, which is exactly the geometry that separates a correct
// containment check from a naive strings.HasPrefix.
//
// "innocent.png" and "linkdot" are the geometry for the other axis. Both stay
// INSIDE the vault, so containment cannot refuse them; both name no dot segment
// in the request, so the request-path scan cannot refuse them either. Only a
// scan of the RESOLVED path denies them. They are the fixtures that separate a
// dot rule about the file actually opened from one about the string asked for.
func assetVault(test *testing.T) string {
	test.Helper()

	base := test.TempDir()
	root := filepath.Join(base, "vault")

	mkdir := func(path string) {
		test.Helper()

		if mkdirErr := os.MkdirAll(path, 0o755); mkdirErr != nil {
			test.Fatalf("mkdir %s: %v", path, mkdirErr)
		}
	}

	write := func(path, content string) {
		test.Helper()

		if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", path, writeErr)
		}
	}

	link := func(target, name string) {
		test.Helper()

		if linkErr := os.Symlink(target, name); linkErr != nil {
			test.Fatalf("symlink %s -> %s: %v", name, target, linkErr)
		}
	}

	mkdir(filepath.Join(root, "img"))
	mkdir(filepath.Join(root, "sub"))
	mkdir(filepath.Join(root, ".tusk"))
	mkdir(filepath.Join(root, ".git"))
	mkdir(filepath.Join(base, "vault-secrets"))
	mkdir(filepath.Join(base, "outside"))

	write(filepath.Join(root, "img", "x.png"), "PNG")
	write(filepath.Join(root, "index.html"), "<b>vault asset</b>")
	write(filepath.Join(root, ".tusk", "index.db"), secretBody)
	write(filepath.Join(root, ".git", "config"), secretBody)
	write(filepath.Join(root, ".hidden"), secretBody)
	write(filepath.Join(base, "vault-secrets", "creds.txt"), secretBody)
	write(filepath.Join(base, "outside", "secret.txt"), secretBody)

	link(filepath.Join(root, "img", "x.png"), filepath.Join(root, "inside.png"))
	link(filepath.Join(base, "outside", "secret.txt"), filepath.Join(root, "escape.txt"))
	link(filepath.Join(base, "vault-secrets", "creds.txt"), filepath.Join(root, "sneaky.txt"))
	link(filepath.Join(root, ".tusk", "index.db"), filepath.Join(root, "innocent.png"))
	link(filepath.Join(root, ".git"), filepath.Join(root, "linkdot"))

	return root
}

// assetServer starts the real server over the fixture vault and returns a
// client that does not follow redirects, so a 3xx is observed rather than
// chased into some other route's answer.
//
// It deliberately drives srv.Handler() — the real mux — rather than calling
// handleAsset with req.SetPathValue. The whole security posture of this route
// runs through the {path...} wildcard and net/http's unescaping of it, and a
// SetPathValue test hand-feeds the value that machinery would have produced,
// so it cannot observe either. It also cannot observe the host guard, which
// matters here for the opposite reason: httptest.NewRequest defaults Host to
// "example.com", which the loopback guard 403s — the exact code these denial
// tests expect. Driving a real listener keeps Host at 127.0.0.1 so a 403 can
// only have come from the asset guard.
func assetServer(test *testing.T) (*httptest.Server, *http.Client) {
	test.Helper()

	server := httptest.NewServer(New(Deps{Root: assetVault(test)}).Handler())
	test.Cleanup(server.Close)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return server, client
}

// getAsset issues one request and returns its status and body. The body is
// returned on every path, including denials, so callers can assert the secret
// never rode along with a non-200.
func getAsset(test *testing.T, server *httptest.Server, client *http.Client, target string) (int, string) {
	test.Helper()

	resp, getErr := client.Get(server.URL + target)

	if getErr != nil {
		test.Fatalf("GET %s: %v", target, getErr)
	}

	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)

	if readErr != nil {
		test.Fatalf("read %s: %v", target, readErr)
	}

	return resp.StatusCode, string(body)
}

// TestAssetServesRegularFile pins the happy path: the bytes actually arrive,
// with a Content-Type derived from the extension. Asserting the status alone
// would let a guard that serves an empty 200 pass.
func TestAssetServesRegularFile(test *testing.T) {
	server, client := assetServer(test)

	resp, getErr := client.Get(server.URL + "/api/asset/img/x.png")

	if getErr != nil {
		test.Fatalf("GET: %v", getErr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		test.Fatalf("code=%d want 200", resp.StatusCode)
	}

	body, readErr := io.ReadAll(resp.Body)

	if readErr != nil {
		test.Fatalf("read: %v", readErr)
	}

	if string(body) != "PNG" {
		test.Fatalf("body=%q want %q", body, "PNG")
	}

	if contentType := resp.Header.Get("Content-Type"); contentType != "image/png" {
		test.Fatalf("content-type=%q want image/png", contentType)
	}
}

// TestAssetAllowsSymlinkInsideVault pins the other side of the containment
// check: EvalSymlinks resolves an in-vault symlink to an in-vault target, which
// is still inside the root and must be served. A guard that rejected every
// symlink would pass every escape test in this file while quietly breaking a
// legitimate vault layout.
func TestAssetAllowsSymlinkInsideVault(test *testing.T) {
	server, client := assetServer(test)

	code, body := getAsset(test, server, client, "/api/asset/inside.png")

	if code != http.StatusOK || body != "PNG" {
		test.Fatalf("in-vault symlink: code=%d body=%q want 200/PNG", code, body)
	}
}

// TestAssetBlocksTraversal drives every denial through the real mux and asserts
// the SPECIFIC status each one must produce. "not 200" is not good enough: this
// route can answer non-200 for reasons that have nothing to do with the guard
// (the mux's own redirect, http.ServeFile's "invalid URL path" 400, the host
// guard's 403), so a test that only rejects 200 would still pass against a
// handler with no guard at all.
func TestAssetBlocksTraversal(test *testing.T) {
	server, client := assetServer(test)

	cases := []struct {
		name   string
		target string
		want   int
	}{
		{
			// The mux cleans the path and redirects before any handler runs, so
			// the guard never sees this one. Pinned at 307 rather than "non-200"
			// to record that fact: if a future mux change stopped redirecting,
			// this case would start exercising the guard and the code would move
			// to 403 — either is safe, but the test should say which is happening.
			name:   "literal .. is redirected by the mux before the guard",
			target: "/api/asset/../secret.txt",
			want:   http.StatusTemporaryRedirect,
		},
		{
			// The real vector. %2F survives the mux's clean-and-redirect check
			// (which reads EscapedPath), then PathValue unescapes it, so ".."
			// arrives at the guard intact.
			name:   "percent-encoded ..%2F reaches the guard and is refused",
			target: "/api/asset/..%2Fsecret.txt",
			want:   http.StatusForbidden,
		},
		{
			// Same equivalence class, different encoding: %2e%2e unescapes to
			// ".." after the redirect check has already passed.
			name:   "percent-encoded %2e%2e reaches the guard and is refused",
			target: "/api/asset/%2e%2e/secret.txt",
			want:   http.StatusForbidden,
		},
		{
			name:   "the index database is not an asset",
			target: "/api/asset/.tusk/index.db",
			want:   http.StatusForbidden,
		},
		{
			// The dot-prefix check is case-immune by construction ("." has no
			// case variant), which matters because APFS is case-insensitive and
			// EvalSymlinks does not canonicalize case: .TUSK/index.db opens the
			// real file on this host, so a guard matching the literal ".tusk"
			// would be bypassed by shouting.
			name:   "the index database is not an asset in any case",
			target: "/api/asset/.TUSK/index.db",
			want:   http.StatusForbidden,
		},
		{
			name:   "dotfiles are not assets",
			target: "/api/asset/.hidden",
			want:   http.StatusForbidden,
		},
		{
			name:   "a symlink out of the vault is refused",
			target: "/api/asset/escape.txt",
			want:   http.StatusForbidden,
		},
		{
			// The prefix trap. The resolved target is
			// "<base>/vault-secrets/creds.txt", which has "<base>/vault" as a
			// string prefix but is not inside it. A naive
			// strings.HasPrefix(resolved, rootResolved) serves this file.
			name:   "a symlink to a sibling sharing the root's name prefix is refused",
			target: "/api/asset/sneaky.txt",
			want:   http.StatusForbidden,
		},
		{
			// The dot trap. Nothing about the REQUEST is dot-prefixed, and the
			// resolved target is genuinely inside the vault, so neither the
			// request-path scan nor the containment check refuses this one — only
			// the scan of the resolved path does. Without it this case answers 200
			// with the index database's bytes.
			name:   "a symlink whose target is inside a dot directory is refused",
			target: "/api/asset/innocent.png",
			want:   http.StatusForbidden,
		},
		{
			// Same hole reached through a symlinked dot-DIRECTORY rather than a
			// file: the dot segment is contributed by the link's target, so it
			// appears only after resolution. .git/config is the payload that makes
			// this worth a Critical — it carries credentials in a cloned vault.
			name:   "a path through a symlink to a dot directory is refused",
			target: "/api/asset/linkdot/config",
			want:   http.StatusForbidden,
		},
		{
			name:   "a directory is not an asset",
			target: "/api/asset/sub",
			want:   http.StatusNotFound,
		},
		{
			name:   "the vault root itself is not an asset",
			target: "/api/asset/",
			want:   http.StatusForbidden,
		},
		{
			name:   "a missing asset is refused",
			target: "/api/asset/img/nope.png",
			want:   http.StatusForbidden,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			code, body := getAsset(test, server, client, testCase.target)

			if code != testCase.want {
				test.Fatalf("GET %s: code=%d want %d (body %q)", testCase.target, code, testCase.want, body)
			}

			// A 403 on this route has two possible authors: the asset guard's
			// http.Error(writer, "forbidden", …) and the host guard's
			// "forbidden: untrusted Host header". assetServer keeps Host at
			// 127.0.0.1 so the latter cannot fire — asserting the body makes that
			// structural rather than a comment, and it is what would catch a future
			// change that made the host guard start answering these.
			if testCase.want == http.StatusForbidden && body != "forbidden\n" {
				test.Fatalf("GET %s: 403 body=%q want %q — this denial is not the asset guard's", testCase.target, body, "forbidden\n")
			}

			if strings.Contains(body, secretBody) {
				test.Fatalf("GET %s: response leaked the secret: %q", testCase.target, body)
			}
		})
	}
}

// TestAssetIndexHTMLRedirects records a quirk this package inherits rather than
// chooses: http.ServeFile 301-redirects any request whose URL path ends in
// "/index.html" to "./", so an asset by that name answers with a redirect
// instead of its bytes. It is pinned here because it is genuinely surprising,
// and because the pin is what would catch it if the route ever did need to
// serve such a file — the fix being http.ServeContent over an open file, which
// does no path handling of its own.
//
// Accepted as-is: ".html" is an indexable extension, so a vault's index.html is
// a node served by /api/node, not an asset. Note the guard runs first and
// allows it — this is ServeFile's behavior, not a denial.
func TestAssetIndexHTMLRedirects(test *testing.T) {
	server, client := assetServer(test)

	code, body := getAsset(test, server, client, "/api/asset/index.html")

	if code != http.StatusMovedPermanently {
		test.Fatalf("index.html: code=%d want 301 (body %q)", code, body)
	}
}

// TestContainedIn holds the containment rule at the only level where it can be
// held at all.
//
// Every other test in this file drives resolveVaultAsset, and none of them can
// fail when containment breaks: filepath.Rel answers "outside the root" with a
// ".."-prefixed path, and the resolved-path scan refuses "..", so containment
// and the scan deny exactly the same set. Weakening the separator here, or
// deleting the call site entirely, leaves resolveVaultAsset's behavior
// unchanged — both mutations passed the whole suite before this test existed.
// The redundancy is intentional and stays; what it costs is falsifiability
// through the front door, and this test is what buys that back.
//
// The geometry is the fixture's, spelled out as strings: "<base>/vault" as the
// root and "<base>/vault-secrets" as the sibling whose path carries the root as
// a STRING prefix while living outside it. That pair is the separator arm, and
// the "sibling sharing the root's name prefix" case below is the one that goes
// red for strings.HasPrefix(resolved, rootResolved).
//
// No filesystem here on purpose: this is a rule about strings, and the paths it
// must judge correctly include ones no TempDir would hand out.
func TestContainedIn(test *testing.T) {
	root := filepath.Join("/base", "vault")

	cases := []struct {
		name     string
		resolved string
		want     bool
	}{
		{
			// A symlink can point back at the root, so the predicate admits it;
			// the caller's IsRegular check is what turns a directory away.
			name:     "the root itself is contained",
			resolved: root,
			want:     true,
		},
		{
			name:     "a file directly under the root is contained",
			resolved: filepath.Join(root, "index.html"),
			want:     true,
		},
		{
			name:     "a nested file is contained",
			resolved: filepath.Join(root, "img", "x.png"),
			want:     true,
		},
		{
			// THE case. "/base/vault" is a string prefix of
			// "/base/vault-secrets", so a separator-less HasPrefix reports this
			// file as contained and the guard serves someone else's credentials.
			// If this test is green under such a check, it has stopped doing its
			// job.
			name:     "a sibling sharing the root's name prefix is not contained",
			resolved: filepath.Join("/base", "vault-secrets", "creds.txt"),
			want:     false,
		},
		{
			// Same trap without a separator anywhere after the root — a sibling
			// FILE rather than a directory, in case a future check reaches for
			// the last separator instead of the root's own boundary.
			name:     "a sibling file sharing the root's name prefix is not contained",
			resolved: filepath.Join("/base", "vaultx"),
			want:     false,
		},
		{
			name:     "an unrelated absolute path is not contained",
			resolved: filepath.Join("/etc", "passwd"),
			want:     false,
		},
		{
			name:     "the root's parent is not contained",
			resolved: "/base",
			want:     false,
		},
		{
			// The root's spelling appearing DEEPER in the path is not
			// containment: prefix, not substring.
			name:     "the root's path appearing as an inner substring is not contained",
			resolved: filepath.Join("/elsewhere", "base", "vault", "img", "x.png"),
			want:     false,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			if got := containedIn(root, testCase.resolved); got != testCase.want {
				test.Fatalf("containedIn(%q, %q) = %v want %v", root, testCase.resolved, got, testCase.want)
			}
		})
	}
}

// TestResolveVaultAssetContainment exercises the guard directly, below the mux.
// The mux redirects some inputs before a handler ever runs (see the literal ".."
// case above), which means those inputs cannot be tested for containment over
// HTTP at all — this is where they get covered.
func TestResolveVaultAssetContainment(test *testing.T) {
	root := assetVault(test)

	cases := []struct {
		name string
		rel  string
		want bool
	}{
		{name: "a plain asset resolves", rel: "img/x.png", want: true},
		{name: "a dot-slash prefix is normalized, not refused", rel: "./img/x.png", want: true},
		{name: "a redundant separator is normalized", rel: "img//x.png", want: true},
		{name: "an in-vault symlink resolves", rel: "inside.png", want: true},
		{name: "empty is refused", rel: "", want: false},
		{name: "a bare dot is refused", rel: ".", want: false},
		{name: "an absolute path is refused", rel: "/etc/passwd", want: false},
		{name: "a parent segment is refused", rel: "../outside/secret.txt", want: false},
		{name: "a deep parent chain is refused", rel: "../../outside/secret.txt", want: false},
		{
			// Refused as authored, NOT rewritten. filepath.Clean("/"+rel) would
			// fold this to "img/x.png" and serve a 200; the guard scans for ".."
			// before cleaning precisely so an escape attempt is answered with a
			// refusal rather than a different file.
			name: "a parent segment that cleans back inside is still refused",
			rel:  "img/../img/x.png",
			want: false,
		},
		{name: "the index directory is refused", rel: ".tusk/index.db", want: false},
		{name: "a dotfile is refused", rel: ".hidden", want: false},
		{name: "a symlink escape is refused", rel: "escape.txt", want: false},
		{name: "a sibling sharing the root's name prefix is refused", rel: "sneaky.txt", want: false},
		{
			// Refused for a dot segment the REQUEST never names — it arrives from
			// the symlink's target, so only the post-resolution scan can see it.
			// Note this one stays inside the root, so the containment assertion
			// below would be satisfied: containment is not what refuses it.
			name: "a symlink into a dot directory is refused",
			rel:  "innocent.png",
			want: false,
		},
		{
			name: "a path through a symlink to a dot directory is refused",
			rel:  "linkdot/config",
			want: false,
		},
		{name: "a missing file is refused", rel: "img/nope.png", want: false},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			resolved, ok := resolveVaultAsset(root, testCase.rel)

			if ok != testCase.want {
				test.Fatalf("resolveVaultAsset(%q) ok=%v want %v (resolved %q)", testCase.rel, ok, testCase.want, resolved)
			}

			if !ok {
				return
			}

			// A resolved path must be inside the root even when ok is true —
			// otherwise the containment check is reporting success on an escape.
			rootResolved, evalErr := filepath.EvalSymlinks(root)

			if evalErr != nil {
				test.Fatalf("eval root: %v", evalErr)
			}

			if !strings.HasPrefix(resolved, rootResolved+string(filepath.Separator)) {
				test.Fatalf("resolveVaultAsset(%q) = %q, outside root %q", testCase.rel, resolved, rootResolved)
			}
		})
	}
}
