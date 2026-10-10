package pathref

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Disk answers whether workspace paths exist, matching every segment's case
// exactly so a case-insensitive filesystem (APFS, NTFS) gives the answer a
// case-sensitive one would. It caches each directory listing it reads, so make
// one per pass (a doctor run, a query, a reap) instead of keeping one around.
// A Disk is not safe for concurrent use.
type Disk struct {
	root string
	// listings maps a workspace-relative directory ("" is the root) to its
	// entries by name. A nil map records a directory that could not be read.
	listings map[string]map[string]fs.DirEntry
}

// NewDisk returns a Disk over the workspace rooted at root.
func NewDisk(root string) *Disk {
	return &Disk{root: root, listings: map[string]map[string]fs.DirEntry{}}
}

// Anchored reports whether target starts somewhere real: its first segment
// exists at the workspace root, and is a directory when more segments follow.
// A span that isn't anchored names nothing in this workspace (`application/json`,
// `github.com/x/y`) and is never treated as a path.
func (disk *Disk) Anchored(target string) bool {
	first, rest, nested := strings.Cut(target, "/")
	entries := disk.listing("")

	entry, found := entries[first]

	if !found {
		return false
	}

	return !nested || rest == "" || disk.isDir("", entry)
}

// Present reports whether target exists with every segment's case exact. A
// directory on the way that can't be read counts as present, since it can't
// prove a mismatch; callers only ever flag a path as missing on proof.
func (disk *Disk) Present(target string) bool {
	segments := strings.Split(target, "/")
	dir := ""

	for index, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}

		entries := disk.listing(dir)

		if entries == nil {
			return true
		}

		entry, found := entries[segment]

		if !found {
			return false
		}

		if index < len(segments)-1 && !disk.isDir(dir, entry) {
			return false
		}

		dir = path.Join(dir, segment)
	}

	return true
}

// listing returns dir's entries by name, reading the directory on first use.
// It returns nil for a directory that can't be read.
func (disk *Disk) listing(dir string) map[string]fs.DirEntry {
	if entries, cached := disk.listings[dir]; cached {
		return entries
	}

	read, readErr := os.ReadDir(filepath.Join(disk.root, filepath.FromSlash(dir)))

	if readErr != nil {
		disk.listings[dir] = nil

		return nil
	}

	entries := make(map[string]fs.DirEntry, len(read))

	for _, entry := range read {
		entries[entry.Name()] = entry
	}

	disk.listings[dir] = entries

	return entries
}

// isDir reports whether entry, found in dir, is a directory or a symlink to one.
func (disk *Disk) isDir(dir string, entry fs.DirEntry) bool {
	if entry.IsDir() {
		return true
	}

	if entry.Type()&fs.ModeSymlink == 0 {
		return false
	}

	info, statErr := os.Stat(filepath.Join(disk.root, filepath.FromSlash(dir), entry.Name()))

	return statErr == nil && info.IsDir()
}
