package wikilink

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// A wikilink target names its node in one of three forms (#791):
//
//   - a workspace path, `[[notes/auth-rfc]]`: the node id itself;
//   - a relative path, `[[./auth-rfc]]` or `[[../notes/auth-rfc]]`: a path
//     from the linking file's folder, Obsidian's "relative path" link format;
//   - a name, `[[auth-rfc]]` or `[[Auth RFC]]`: no folder at all, Obsidian's
//     "shortest path when possible" format. The exact id still wins, so a link
//     that resolved before name lookup existed resolves the same way; failing
//     that, the name is matched against every file's name (its base name
//     without the extension), and one match resolves the link.
//
// A `#fragment` (a sub-unit address such as `#S1`) is never part of the path:
// it is cut off before resolution and put back on the id after.

// Case says how a name-form link's name is compared with file names.
type Case string

const (
	// CaseInsensitive matches a name in any letter case, as Obsidian does on
	// every operating system.
	CaseInsensitive Case = "insensitive"
	// CaseSensitive matches a name only in the case the file name has.
	CaseSensitive Case = "sensitive"
)

// DefaultCase applies when the manifest leaves [workspace] link-case unset.
const DefaultCase = CaseInsensitive

// ParseCase resolves a manifest value to a Case. The empty string is the
// default; any other value outside the two cases is an error.
func ParseCase(value string) (Case, error) {
	switch Case(value) {
	case "":
		return DefaultCase, nil
	case CaseInsensitive, CaseSensitive:
		return Case(value), nil
	}

	return "", fmt.Errorf("link-case = %q is not supported (want %s | %s)", value, CaseInsensitive, CaseSensitive)
}

// SplitFragment cuts a target at its first `#` into the path part and the
// fragment, `#` included ("" when the target has none).
func SplitFragment(target string) (pathPart, fragment string) {
	if hash := strings.IndexByte(target, '#'); hash >= 0 {
		return target[:hash], target[hash:]
	}

	return target, ""
}

// IsName reports whether target names a file by name: its path part is not
// empty, holds no `/`, and is not the `.` or `..` of a relative path.
func IsName(target string) bool {
	pathPart, _ := SplitFragment(target)

	return pathPart != "" && pathPart != "." && pathPart != ".." && !strings.Contains(pathPart, "/")
}

// IsRelative reports whether target is written from the linking file's folder:
// its path part starts with `./` or `../`.
func IsRelative(target string) bool {
	return strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../")
}

// relative resolves a relative target from sourcePath, the linking file's
// workspace-relative path, keeping the fragment. It reports false when target
// is not relative or climbs out of the workspace.
func relative(target, sourcePath string) (string, bool) {
	if !IsRelative(target) {
		return "", false
	}

	pathPart, fragment := SplitFragment(target)
	joined := path.Join(path.Dir(sourcePath), pathPart)

	if joined == ".." || strings.HasPrefix(joined, "../") || joined == "." {
		return "", false
	}

	return joined + fragment, true
}

// Normalize returns what an edge stores for a link to target written in the
// file at sourcePath, before any name is looked up: a relative target becomes
// the id it names, and every other target stays as written. A relative target
// that climbs out of the workspace also stays as written, so its edge dangles
// and doctor reports it.
func Normalize(target, sourcePath string) string {
	if resolved, isRelative := relative(target, sourcePath); isRelative {
		return resolved
	}

	return target
}

// RelativeTo renders id as a relative link from the folder of the file at
// sourcePath: `./` and the rest of the path when id sits in that folder or
// below it, otherwise one `../` per folder to climb. Normalize of the result
// from sourcePath is id again.
func RelativeTo(sourcePath, id string) string {
	fromDir := path.Dir(sourcePath)

	var fromSegments []string

	if fromDir != "." {
		fromSegments = strings.Split(fromDir, "/")
	}

	targetSegments := strings.Split(id, "/")
	common := 0

	for common < len(fromSegments) && common < len(targetSegments)-1 && fromSegments[common] == targetSegments[common] {
		common++
	}

	climb := len(fromSegments) - common

	if climb == 0 {
		return "./" + strings.Join(targetSegments[common:], "/")
	}

	return strings.Repeat("../", climb) + strings.Join(targetSegments[common:], "/")
}

// FileName returns the name a file answers to in a name-form link: its base
// name without the extension. `notes/Auth RFC.md` answers to `Auth RFC`.
func FileName(relPath string) string {
	base := path.Base(relPath)

	return strings.TrimSuffix(base, path.Ext(base))
}

// NameKey folds a name for comparison: what nodes.name_key stores, so a
// case-insensitive lookup is one indexed equality.
func NameKey(name string) string {
	return strings.ToLower(name)
}

// Files answers which file a name-form link names. *Names answers from memory;
// the index answers one name at a time from its name_key column.
type Files interface {
	// ResolveName returns the id of the file name names: the file whose id is
	// name itself, else the only file whose name matches. When none or several
	// match, id is "" and candidates lists the matches, sorted.
	ResolveName(name string) (id string, candidates []string, err error)
}

// File is one candidate for a name: a file node's id and its path.
type File struct {
	ID   string
	Path string
}

// MatchName is the decision every Files implementation shares. exact says a
// file has name as its id; named are the files whose NameKey equals name's,
// which MatchName narrows to the exact case under CaseSensitive.
func MatchName(name string, linkCase Case, exact bool, named []File) (string, []string) {
	if exact {
		return name, nil
	}

	var candidates []string

	for _, file := range named {
		if linkCase == CaseSensitive && FileName(file.Path) != name {
			continue
		}

		candidates = append(candidates, file.ID)
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}

	sort.Strings(candidates)

	return "", candidates
}

// Resolve returns the node id target names when it is linked from the file at
// sourcePath. A workspace path is returned as written: whether a node has that
// id is the caller's question. A relative path is resolved from sourcePath's
// folder, and one that climbs out of the workspace resolves to "". A name is
// looked up through files; when none or several files match, id is "" and
// candidates lists the matches.
func Resolve(target, sourcePath string, files Files) (string, []string, error) {
	if IsRelative(target) {
		resolved, _ := relative(target, sourcePath)

		return resolved, nil, nil
	}

	if !IsName(target) {
		return target, nil, nil
	}

	pathPart, fragment := SplitFragment(target)
	id, candidates, resolveErr := files.ResolveName(strings.TrimSpace(pathPart))

	if resolveErr != nil || id == "" {
		return "", candidates, resolveErr
	}

	return id + fragment, nil, nil
}

// Names is an in-memory index of the workspace's file nodes by name. Reindex
// and doctor load it once and resolve every name-form link in the vault
// against it, and a move compares one taken before the move with one taken
// after.
type Names struct {
	linkCase Case
	paths    map[string]string   // file id → its path
	byKey    map[string][]string // NameKey of a file name → ids
}

// NewNames returns an empty Names that matches under linkCase.
func NewNames(linkCase Case) *Names {
	return &Names{linkCase: linkCase, paths: map[string]string{}, byKey: map[string][]string{}}
}

// Add records the file node id stored at relPath.
func (names *Names) Add(id, relPath string) {
	if _, known := names.paths[id]; known {
		names.Remove(id)
	}

	key := NameKey(FileName(relPath))
	names.paths[id] = relPath
	names.byKey[key] = append(names.byKey[key], id)
}

// Remove forgets the file node id.
func (names *Names) Remove(id string) {
	relPath, known := names.paths[id]

	if !known {
		return
	}

	delete(names.paths, id)

	key := NameKey(FileName(relPath))
	kept := names.byKey[key][:0:0]

	for _, existing := range names.byKey[key] {
		if existing != id {
			kept = append(kept, existing)
		}
	}

	if len(kept) == 0 {
		delete(names.byKey, key)

		return
	}

	names.byKey[key] = kept
}

// Clone returns an independent copy.
func (names *Names) Clone() *Names {
	cloned := NewNames(names.linkCase)

	for id, relPath := range names.paths {
		cloned.Add(id, relPath)
	}

	return cloned
}

// Path returns the path of the file node id, and false when Names has none.
func (names *Names) Path(id string) (string, bool) {
	relPath, known := names.paths[id]

	return relPath, known
}

// ResolveName implements Files.
func (names *Names) ResolveName(name string) (string, []string, error) {
	_, exact := names.paths[name]

	named := make([]File, 0, len(names.byKey[NameKey(name)]))

	for _, id := range names.byKey[NameKey(name)] {
		named = append(named, File{ID: id, Path: names.paths[id]})
	}

	id, candidates := MatchName(name, names.linkCase, exact, named)

	return id, candidates, nil
}
