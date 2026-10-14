package index

import (
	"database/sql"
	"fmt"

	"github.com/germanamz/tusk/internal/wikilink"
)

// nameKeyForPath is the nodes.name_key of a file row stored at relPath.
func nameKeyForPath(relPath string) string {
	return wikilink.NameKey(wikilink.FileName(relPath))
}

// scanFiles drains (id, path) rows. It closes rows before returning.
func scanFiles(rows *sql.Rows) ([]wikilink.File, error) {
	defer rows.Close()

	var files []wikilink.File

	for rows.Next() {
		var file wikilink.File

		if scanErr := rows.Scan(&file.ID, &file.Path); scanErr != nil {
			return nil, scanErr
		}

		files = append(files, file)
	}

	return files, rows.Err()
}

// LinkNames loads every file node into a wikilink.Names that matches under
// linkCase: the snapshot ResolveLinks and doctor resolve a whole vault's
// name-form links against, one query instead of one per link.
func (repo *NodeRepo) LinkNames(linkCase wikilink.Case) (*wikilink.Names, error) {
	rows, queryErr := repo.db.Query(`SELECT id, path FROM nodes WHERE kind = 'file'`)

	if queryErr != nil {
		return nil, fmt.Errorf("nodeRepo: link names: %w", queryErr)
	}

	files, scanErr := scanFiles(rows)

	if scanErr != nil {
		return nil, fmt.Errorf("nodeRepo: link names: %w", scanErr)
	}

	names := wikilink.NewNames(linkCase)

	for _, file := range files {
		names.Add(file.ID, file.Path)
	}

	return names, nil
}

// FileNames answers name-form links from the index one name at a time, through
// nodes.name_key. It is the wikilink.Files for paths that resolve a handful of
// links (one note's refs, one page in the reader) and would waste a full
// LinkNames load.
type FileNames struct {
	repo     *NodeRepo
	linkCase wikilink.Case
}

// FileNames returns the index-backed wikilink.Files matching under linkCase.
func (repo *NodeRepo) FileNames(linkCase wikilink.Case) FileNames {
	return FileNames{repo: repo, linkCase: linkCase}
}

// ResolveName implements wikilink.Files.
func (files FileNames) ResolveName(name string) (string, []string, error) {
	var exact int

	if scanErr := files.repo.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE id = ? AND kind = 'file'`, name,
	).Scan(&exact); scanErr != nil {
		return "", nil, fmt.Errorf("nodeRepo: resolve name %q: %w", name, scanErr)
	}

	rows, queryErr := files.repo.db.Query(
		`SELECT id, path FROM nodes WHERE kind = 'file' AND name_key = ?`, wikilink.NameKey(name),
	)

	if queryErr != nil {
		return "", nil, fmt.Errorf("nodeRepo: resolve name %q: %w", name, queryErr)
	}

	named, scanErr := scanFiles(rows)

	if scanErr != nil {
		return "", nil, fmt.Errorf("nodeRepo: resolve name %q: %w", name, scanErr)
	}

	id, candidates := wikilink.MatchName(name, files.linkCase, exact > 0, named)

	return id, candidates, nil
}

// linkEdge is one name-form edge row as ResolveLinks reads it.
type linkEdge struct {
	rowID      int64
	edgeType   string
	sourceID   string
	targetID   string
	sourcePath string
	kind       string
	link       string
}

// ResolveLinks points every name-form edge (link IS NOT NULL) at the file its
// link names in names: the file with that id, else the only file with that
// name, with the link's #fragment put back. A link that names no file, or
// several, keeps its own text as the target, so its edge dangles and doctor
// tells the two apart. Run it after anything that writes such edges or changes
// the set of files: it is the one place a name is looked up, so a vault indexed
// in any order, or a file added, removed or renamed out of band, converges in
// one pass without re-parsing any file.
//
// A re-pointed edge that duplicates an edge its source already has (the same
// note linked by name and by path) is deleted rather than kept twice. Returns
// how many rows changed.
func (repo *EdgeRepo) ResolveLinks(names *wikilink.Names) (int, error) {
	rows, queryErr := repo.db.Query(
		`SELECT id, type, source_id, target_id, source_path, kind, link FROM edges WHERE link IS NOT NULL`,
	)

	if queryErr != nil {
		return 0, fmt.Errorf("edgeRepo: resolve links: %w", queryErr)
	}

	var stale []linkEdge

	scanErr := func() error {
		defer rows.Close()

		for rows.Next() {
			var edge linkEdge

			if scanErr := rows.Scan(&edge.rowID, &edge.edgeType, &edge.sourceID, &edge.targetID, &edge.sourcePath, &edge.kind, &edge.link); scanErr != nil {
				return scanErr
			}

			target := edge.link

			if resolved, _, _ := wikilink.Resolve(edge.link, edge.sourcePath, names); resolved != "" {
				target = resolved
			}

			if target != edge.targetID {
				edge.targetID = target
				stale = append(stale, edge)
			}
		}

		return rows.Err()
	}()

	if scanErr != nil {
		return 0, fmt.Errorf("edgeRepo: resolve links: scan: %w", scanErr)
	}

	if len(stale) == 0 {
		return 0, nil
	}

	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return 0, fmt.Errorf("edgeRepo: resolve links: begin: %w", beginErr)
	}

	for _, edge := range stale {
		if _, execErr := tx.Exec(`UPDATE edges SET target_id = ? WHERE id = ?`, edge.targetID, edge.rowID); execErr != nil {
			_ = tx.Rollback()

			return 0, fmt.Errorf("edgeRepo: resolve link %q from %s: %w", edge.link, edge.sourceID, execErr)
		}
	}

	// Dedupe once every row has moved: checking row by row would let a row
	// that is about to move away from a target count as that target's
	// survivor. A path-written edge always wins over a name-form twin; among
	// name-form twins the oldest row stays.
	if _, execErr := tx.Exec(`
		DELETE FROM edges WHERE link IS NOT NULL AND EXISTS (
			SELECT 1 FROM edges AS other
			WHERE other.type = edges.type AND other.source_id = edges.source_id
			  AND other.target_id = edges.target_id AND other.source_path = edges.source_path
			  AND other.kind = edges.kind AND other.id != edges.id
			  AND (other.link IS NULL OR other.id < edges.id)
		)`); execErr != nil {
		_ = tx.Rollback()

		return 0, fmt.Errorf("edgeRepo: resolve links: dedupe: %w", execErr)
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return 0, fmt.Errorf("edgeRepo: resolve links: commit: %w", commitErr)
	}

	return len(stale), nil
}

// RenameLink re-points the name-form edges the file at sourcePath declares
// with the link text oldName (its own, or oldName#fragment) after a rewrite of
// the file changed that text to newName: `tusk node move` rewrote [[old]] to
// [[new]], or to a full id. The rewrite re-derives only the file's own rows, so
// without this its sub-units' rows would keep resolving the old text. The new
// text is stored as the target with its fragment, and as the link when it is
// itself a name; a full id is a plain edge. Run ResolveLinks after.
func (repo *EdgeRepo) RenameLink(sourcePath, oldName, newName string) error {
	var link any

	if wikilink.IsName(newName) {
		link = newName
	}

	if _, execErr := repo.db.Exec(`
		UPDATE edges
		SET target_id = ?1 || substr(link, length(?2) + 1),
		    link      = CASE WHEN ?3 IS NULL THEN NULL ELSE ?1 || substr(link, length(?2) + 1) END
		WHERE source_path = ?4 AND (link = ?2 OR substr(link, 1, length(?2) + 1) = ?2 || '#')`,
		newName, oldName, link, sourcePath,
	); execErr != nil {
		return fmt.Errorf("edgeRepo: rename link %q in %s: %w", oldName, sourcePath, execErr)
	}

	return nil
}

// ResolveLinks loads the file names under linkCase and re-points every
// name-form edge with EdgeRepo.ResolveLinks. A nil repo is a no-op.
func ResolveLinks(nodes *NodeRepo, edges *EdgeRepo, linkCase wikilink.Case) error {
	if nodes == nil || edges == nil {
		return nil
	}

	names, namesErr := nodes.LinkNames(linkCase)

	if namesErr != nil {
		return namesErr
	}

	_, resolveErr := edges.ResolveLinks(names)

	return resolveErr
}
