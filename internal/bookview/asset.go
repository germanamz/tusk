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
// will not answer: a path that escapes the vault, one that names OR RESOLVES TO
// a dotfile or dot-directory (.tusk, .git), or one that resolves to nothing.
//
// "names or resolves to" is why the dot rule is checked twice, against two
// different inputs. See the second scan, at the bottom.
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
	//
	// This scan sees only what the request SPELLS. It cannot see a dot segment a
	// symlink introduces, so it is necessary but not sufficient — the same rule is
	// re-applied to the resolved path at the bottom of this function.
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

	// Containment, checked on the resolved paths. Extracted because it cannot be
	// falsified through this function — see containedIn's own comment and
	// TestContainedIn, which is where its separator arm is actually held.
	if !containedIn(rootResolved, resolved) {
		return "", false
	}

	// Apply the dot rule a SECOND time, now to the resolved path. This is not a
	// duplicate of the scan above, and deleting either one opens a hole the other
	// does not cover.
	//
	// The scan above reads the REQUEST path, and a request path cannot see through
	// a symlink. "innocent.png" names no dot segment, so it passes that scan; if it
	// is a symlink to "<root>/.tusk/index.db", EvalSymlinks lands on the index,
	// which IS inside the vault — so containment above is satisfied too, and the
	// index gets served. Nothing has escaped the vault; the dot rule has simply
	// been walked around, because it was checked against the string the caller
	// asked for rather than the file the kernel would open. The rule is about the
	// file, so it has to be re-asked of the path that names it. A symlinked
	// dot-DIRECTORY ("linkdot" -> ".git", then "linkdot/config") is the same hole
	// with the dot segment in the middle.
	//
	// The two scans are split by input, and each is the only one that can catch its
	// own case: the request scan runs pre-resolution, the sole point where ".." is
	// still visible as authored (Clean and EvalSymlinks both fold it away), and
	// this one runs post-resolution, the sole point where a link's target is
	// visible at all.
	//
	// Rel, not TrimPrefix: only the segments BELOW the root are the request's to
	// name. The root itself may legitimately sit inside a dot-directory — a vault
	// at "~/.local/share/notes" is ordinary — and scanning the absolute path would
	// refuse every asset in it.
	//
	// Known bound, not an oversight: the dot rule is a rule about PATHS. A HARD
	// link at "<root>/hard.png" pointing at "<root>/.tusk/index.db" is served —
	// EvalSymlinks cannot see through a hard link, so the resolved path names no
	// dot segment and there is nothing here to catch. This is inherent to any
	// path-based rule (os.Root would not close it either), and it is bounded: it
	// needs local write access inside the vault, and unlike symlinks, git and
	// most sync tools do not carry hard links, so the "cloned vault" vector — the
	// one that makes .git/config worth a Critical — does not reach it.
	relResolved, relErr := filepath.Rel(rootResolved, resolved)

	if relErr != nil {
		return "", false
	}

	for _, segment := range strings.Split(relResolved, string(filepath.Separator)) {
		// Unreachable while containment stands: Rel spells "not under the root"
		// with a leading "..", and containedIn refused every such path above.
		// Written out anyway, and it must stay — deleting it is not a cleanup.
		//
		// The dot rule below would otherwise swallow ".." by pure coincidence,
		// since ".." happens to start with ".". That coincidence is nobody's
		// intent, and leaving it implicit is what let this arm rot once already:
		// it makes containment a no-op through this function, so a reviewer who
		// deletes containment "as redundant" sees green, and a reviewer who then
		// simplifies the dot rule to skip ".." — reasonable on its face, since
		// ".." is not a dotfile and the pre-Clean scan already refused it as
		// AUTHORED — sees green too. Each edit is green alone; together they
		// restore the "/vault" vs "/vault-secrets" hole. Answering the two
		// questions separately is what keeps either edit from being quiet.
		if segment == ".." {
			return "", false
		}

		// Rel spells "the root itself" as ".", which is the resolved ==
		// rootResolved arm containedIn admits (a symlink pointing back at the
		// root). That "." is Rel's notation, not a dot segment the request named,
		// so it must not be refused here — the caller's IsRegular check is what
		// turns the root away.
		if strings.HasPrefix(segment, ".") && segment != "." {
			return "", false
		}
	}

	return resolved, true
}

// containedIn reports whether resolved names the vault root itself or something
// beneath it. Both arguments must already be absolute and symlink-resolved —
// this is a string rule, and it is only meaningful over paths the kernel would
// actually open.
//
// The separator is the entire content of this function. A bare
// strings.HasPrefix(resolved, rootResolved) serves "<base>/vault-secrets/creds.txt"
// to a vault rooted at "<base>/vault", because "<base>/vault" is a string prefix
// of "<base>/vault-secrets" — while "<base>/vault/" is not.
//
// It is a free function with its own table test for a reason worth stating,
// because the reason is not obvious and the last round lost to it. This
// predicate cannot be falsified through resolveVaultAsset: filepath.Rel returns
// a ".."-prefixed path for anything outside the root, and the resolved-path scan
// refuses "..", so every input this check would deny is already denied one step
// later — weaken the separator, or delete the call site outright, and
// resolveVaultAsset's behavior does not move. That redundancy is deliberate
// (defence in depth on the one route that touches request-supplied paths), but
// it means a test driving resolveVaultAsset can never hold this rule. Only a
// test that calls this function can. TestContainedIn is that test.
//
// The resolved == rootResolved arm admits the root directory itself, which a
// symlink can point back at; the caller's IsRegular check is what refuses it.
func containedIn(rootResolved, resolved string) bool {
	return resolved == rootResolved || strings.HasPrefix(resolved, rootResolved+string(filepath.Separator))
}
