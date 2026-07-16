package bookview

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleAsset serves one vault file verbatim, so the reader can render an image
// a note references (`![](img/pic.png)`) with a plain <img src>.
//
// This is the only route in the repo that hands a request-supplied path to the
// filesystem — every other read resolves a path from the index walk, which is
// vault-relative by construction. The security therefore lives entirely in
// resolveVaultAsset, not here: by the time http.ServeFile runs, the path has
// been proven to name a regular file inside the vault.
func (srv *Server) handleAsset(writer http.ResponseWriter, request *http.Request) {
	// PathValue unescapes the wildcard, so "..%2F" and "%2e%2e" both arrive as
	// a literal "..". The guard sees the decoded path, which is the one that
	// reaches the filesystem.
	resolved, ok := resolveVaultAsset(srv.deps.Root, request.PathValue("path"))

	if !ok {
		// Escape, dotfile, and "no such file" all answer alike, so a refusal
		// does not report which of the three it was. The reader cannot act on
		// the difference anyway — a broken <img> renders the same either way.
		http.Error(writer, "forbidden", http.StatusForbidden)

		return
	}

	info, statErr := os.Stat(resolved)

	if statErr != nil || !info.Mode().IsRegular() {
		// Directories, devices, sockets and fifos are not assets. Handing a
		// directory to http.ServeFile would serve its listing; opening a fifo
		// would wedge the handler until a writer showed up.
		//
		// This 404 is the one thing a caller can distinguish from the 403 above,
		// so the pair does leak "an in-vault non-regular file exists at this
		// path". Accepted: it is a loopback, read-only server whose reader is
		// already entitled to the whole vault, and the answer is confined to
		// paths that survived the guard — nothing outside the vault, and nothing
		// dot-prefixed, can be probed this way.
		http.NotFound(writer, request)

		return
	}

	// ServeFile derives Content-Type from the extension (falling back to
	// sniffing the first 512 bytes) and adds Range and If-Modified-Since
	// handling. Its own path defenses do not apply to us and are not relied on:
	// its ".." rejection reads request.URL.Path, not the path we pass, and the
	// guard has already refused anything it would catch.
	//
	// Known quirk, accepted: ServeFile 301-redirects any request whose URL path
	// ends in "/index.html" to "./" (stdlib localRedirect), so an asset named
	// index.html answers with a redirect rather than its bytes. Harmless here —
	// .html is an indexable extension, so such a file is a node served by
	// /api/node, not an asset — and pinned by TestAssetIndexHTMLRedirects so the
	// behavior is recorded rather than rediscovered. Switching to ServeContent
	// over an open file is the one-line fix if a vault ever needs it.
	http.ServeFile(writer, request, resolved)
}

// resolveVaultAsset maps a request-supplied, vault-relative asset path to the
// absolute on-disk path to serve, reporting ok=false for anything this server
// will not answer: a path that escapes the vault, names a dotfile or
// dot-directory (.tusk, .git), or resolves to nothing.
//
// It fails closed. Every error path — including one that merely means "this
// file is not there" — returns ok=false, because the only caller is an HTTP
// handler that opens whatever comes back.
func resolveVaultAsset(root, rel string) (string, bool) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", false
	}

	// Refuse ".." as authored, BEFORE cleaning. The order is the whole point.
	// filepath.Clean("/"+rel) absorbs every parent segment — "../secret.txt"
	// folds to "secret.txt", "a/../../x" to "x" — so a scan placed after it can
	// never see one and is dead code. Cleaning first still contains the request,
	// but it answers an escape attempt by serving a *different* in-vault file
	// instead of refusing it: surprising on its own, and it makes the refusal
	// untestable, since the denial would then depend on the folded path
	// happening not to exist rather than on any check here.
	//
	// rel comes from a URL, so its separator is "/" on every platform.
	for _, segment := range strings.Split(rel, "/") {
		if segment == ".." {
			return "", false
		}
	}

	// Absolutize then clean: collapses "." segments and redundant separators,
	// and guarantees the result cannot climb above the root even if some future
	// encoding slips past the scan above.
	clean := strings.TrimPrefix(filepath.Clean("/"+rel), "/")

	if clean == "" || clean == "." {
		return "", false
	}

	// Refuse dot-prefixed segments: .tusk (the index, its wal, the epoch file),
	// .git, and editor dotfiles are not readable assets. Running this on the
	// CLEANED path is deliberate — bare "." segments are gone by now, so a
	// legitimate "./img/x.png" normalizes instead of being refused.
	//
	// The check is case-immune for free, which is load-bearing on APFS: the
	// filesystem is case-insensitive, so ".TUSK/index.db" opens the real index,
	// and filepath.EvalSymlinks does not canonicalize case — a check against the
	// literal ".tusk", before or after resolution, would miss it. "." has no
	// case variant, so a dot prefix cannot be shouted past.
	for _, segment := range strings.Split(clean, string(filepath.Separator)) {
		if strings.HasPrefix(segment, ".") {
			return "", false
		}
	}

	rootAbs, absErr := filepath.Abs(root)

	if absErr != nil {
		return "", false
	}

	// Every check above is lexical, and a symlink is not: resolve both sides,
	// then re-check containment against what the kernel would actually open.
	//
	// EvalSymlinks errors on a path that does not exist, which is why a missing
	// asset is refused here rather than at the caller's os.Stat.
	resolved, resolveErr := filepath.EvalSymlinks(filepath.Join(rootAbs, clean))

	if resolveErr != nil {
		return "", false
	}

	// The root is resolved too, not just the candidate. This is not symmetry for
	// its own sake: on macOS a vault under /var (every t.TempDir(), and /var/folders
	// generally) sits behind a /var -> /private/var symlink, so resolving only the
	// candidate would compare "/private/var/…/vault/img/x.png" against an
	// unresolved "/var/…/vault" and deny every legitimate asset.
	rootResolved, rootErr := filepath.EvalSymlinks(rootAbs)

	if rootErr != nil {
		return "", false
	}

	// Containment, checked on the resolved paths. The separator is what makes
	// this correct: a bare strings.HasPrefix(resolved, rootResolved) serves
	// "/vault-secrets/creds.txt" to a vault rooted at "/vault", because "/vault"
	// is a string prefix of "/vault-secrets" — while "/vault/" is not.
	//
	// The resolved == rootResolved arm admits the root directory itself (a
	// symlink can point back at it); the caller's IsRegular check refuses it.
	if resolved != rootResolved && !strings.HasPrefix(resolved, rootResolved+string(filepath.Separator)) {
		return "", false
	}

	return resolved, true
}
