package index

import (
	"database/sql"
	"fmt"
	"strings"

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

// Exists implements wikilink.Anchors.
func (files FileNames) Exists(id string) (bool, error) {
	return nodeExists(files.repo.db, id)
}

// Sections implements wikilink.Anchors.
func (files FileNames) Sections(fileID string) ([]wikilink.Section, error) {
	return loadSections(files.repo.db, fileID)
}

// nodeExists reports whether a node row has id.
func nodeExists(db *sql.DB, id string) (bool, error) {
	var count int

	if scanErr := db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ?`, id).Scan(&count); scanErr != nil {
		return false, fmt.Errorf("index: node %q exists: %w", id, scanErr)
	}

	return count > 0, nil
}

// loadSections returns fileID's section rows in document order, with the
// heading key a wikilink fragment is matched on.
func loadSections(db *sql.DB, fileID string) ([]wikilink.Section, error) {
	lo, hi := subUnitIDRange(fileID)

	rows, queryErr := db.Query(
		`SELECT id, COALESCE(parent_id, ''), COALESCE(name_key, '') FROM nodes
		 WHERE id >= ? AND id < ? AND kind = 'subunit' AND type = 'section'
		 ORDER BY ordinal`,
		lo, hi,
	)

	if queryErr != nil {
		return nil, fmt.Errorf("index: sections of %s: %w", fileID, queryErr)
	}

	defer rows.Close()

	var sections []wikilink.Section

	for rows.Next() {
		var section wikilink.Section

		if scanErr := rows.Scan(&section.ID, &section.Parent, &section.Key); scanErr != nil {
			return nil, fmt.Errorf("index: scan section of %s: %w", fileID, scanErr)
		}

		sections = append(sections, section)
	}

	return sections, rows.Err()
}

// passLookup is the wikilink.Lookup one ResolveLinks pass resolves against:
// files from the names snapshot, sub-units and sections from the index, each
// read once per pass however many links name them.
type passLookup struct {
	*wikilink.Names

	db       *sql.DB
	subUnits map[string]bool
	sections map[string][]wikilink.Section
}

func newPassLookup(db *sql.DB, names *wikilink.Names) *passLookup {
	return &passLookup{Names: names, db: db, subUnits: map[string]bool{}, sections: map[string][]wikilink.Section{}}
}

// Exists implements wikilink.Anchors: the snapshot is the authority on files.
func (lookup *passLookup) Exists(id string) (bool, error) {
	if !strings.Contains(id, SubUnitIDSeparator) {
		_, isFile := lookup.Path(id)

		return isFile, nil
	}

	if exists, cached := lookup.subUnits[id]; cached {
		return exists, nil
	}

	exists, existsErr := nodeExists(lookup.db, id)

	if existsErr != nil {
		return false, existsErr
	}

	lookup.subUnits[id] = exists

	return exists, nil
}

// Sections implements wikilink.Anchors.
func (lookup *passLookup) Sections(fileID string) ([]wikilink.Section, error) {
	if sections, cached := lookup.sections[fileID]; cached {
		return sections, nil
	}

	sections, loadErr := loadSections(lookup.db, fileID)

	if loadErr != nil {
		return nil, loadErr
	}

	lookup.sections[fileID] = sections

	return sections, nil
}

// linkEdge is one looked-up edge row as ResolveLinks reads it.
type linkEdge struct {
	rowID      int64
	edgeType   string
	sourceID   string
	targetID   string
	sourcePath string
	kind       string
	link       string
}

// ResolveLinks points every looked-up edge (link IS NOT NULL) at the node its
// link names (wikilink.ResolveTarget): the file a name or path names, with
// names matched in names, then the sub-unit its #fragment names there, by
// structural address or by heading (#808). A link that names no file, or
// several, keeps its own text as the target, so its edge dangles and doctor
// tells the two apart. A fragment that names nothing in an existing file
// leaves the edge on the file. Run it after anything that writes such edges
// or changes the set of files or their headings: it is the one place a link
// is looked up, so a vault indexed in any order, or a file added, removed,
// renamed or edited out of band, converges in one pass without re-parsing any
// file.
//
// A re-pointed edge that duplicates an edge its source already has (the same
// note linked by name and by path) is deleted rather than kept twice, except
// a fallback row, which parks at its fragment instead (see the park step).
// Returns how many rows changed.
func (repo *EdgeRepo) ResolveLinks(names *wikilink.Names) (int, error) {
	rows, queryErr := repo.db.Query(
		`SELECT id, type, source_id, target_id, source_path, kind, link FROM edges WHERE link IS NOT NULL`,
	)

	if queryErr != nil {
		return 0, fmt.Errorf("edgeRepo: resolve links: %w", queryErr)
	}

	var linked []linkEdge

	scanErr := func() error {
		defer rows.Close()

		for rows.Next() {
			var edge linkEdge

			if scanErr := rows.Scan(&edge.rowID, &edge.edgeType, &edge.sourceID, &edge.targetID, &edge.sourcePath, &edge.kind, &edge.link); scanErr != nil {
				return scanErr
			}

			linked = append(linked, edge)
		}

		return rows.Err()
	}()

	if scanErr != nil {
		return 0, fmt.Errorf("edgeRepo: resolve links: scan: %w", scanErr)
	}

	// Resolve after the scan has closed, so the lookup's own queries never run
	// beside an open cursor.
	lookup := newPassLookup(repo.db, names)

	// Plan where every row lands before deciding whether a fallback row parks:
	// that depends on where its twins land in this same pass.
	plans := make([]linkPlan, 0, len(linked))
	landing := map[edgeKey][]int{}

	for _, edge := range linked {
		fileID, _, _ := strings.Cut(edge.sourceID, SubUnitIDSeparator)
		source := wikilink.Source{ID: fileID, Path: edge.sourcePath}
		resolved, resolveErr := wikilink.ResolveTarget(edge.link, source, lookup)

		if resolveErr != nil {
			return 0, fmt.Errorf("edgeRepo: resolve link %q from %s: %w", edge.link, edge.sourceID, resolveErr)
		}

		plan := linkPlan{edge: edge, target: edge.link, fallback: resolved.MissingFragment}

		if resolved.ID != "" {
			plan.target = resolved.ID
		}

		landing[plan.key()] = append(landing[plan.key()], len(plans))
		plans = append(plans, plan)
	}

	var stale []linkEdge

	for _, plan := range plans {
		// A fallback row the park step left waiting beside a twin stays
		// there while the twin lands on the note, or every pass would move it
		// to the note and park it again.
		if plan.fallback {
			_, fragment := wikilink.SplitFragment(plan.edge.link)

			if parked := plan.target + fragment; plan.edge.targetID == parked {
				twinned, twinErr := repo.twinLands(plan, plans, landing[plan.key()])

				if twinErr != nil {
					return 0, twinErr
				}

				if twinned {
					plan.target = parked
				}
			}
		}

		if plan.target != plan.edge.targetID {
			edge := plan.edge
			edge.targetID = plan.target
			stale = append(stale, edge)
		}
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
	//
	// A fallback row, a link whose #fragment named nothing and so landed on
	// its note, is never deleted and never deletes another: the next pass
	// may find its heading and move it off the note, and the twin must still
	// be there when it does. It parks at the fragment it names instead
	// (`notes/auth-rfc#Renamed`, dangling) whenever its source already links
	// the note, so the note is linked once and doctor still sees the miss.
	if _, execErr := tx.Exec(`
		DELETE FROM edges WHERE link IS NOT NULL AND NOT ` + fallbackRow("edges") + ` AND EXISTS (
			SELECT 1 FROM edges AS other
			WHERE ` + sameEdge + `
			  AND NOT ` + fallbackRow("other") + `
			  AND (other.link IS NULL OR other.id < edges.id)
		)`); execErr != nil {
		_ = tx.Rollback()

		return 0, fmt.Errorf("edgeRepo: resolve links: dedupe: %w", execErr)
	}

	if _, execErr := tx.Exec(`
		UPDATE edges SET target_id = target_id || substr(link, instr(link, '#'))
		WHERE ` + fallbackRow("edges") + ` AND EXISTS (
			SELECT 1 FROM edges AS other
			WHERE ` + sameEdge + `
			  AND (NOT ` + fallbackRow("other") + ` OR other.id < edges.id)
		)`); execErr != nil {
		_ = tx.Rollback()

		return 0, fmt.Errorf("edgeRepo: resolve links: park: %w", execErr)
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return 0, fmt.Errorf("edgeRepo: resolve links: commit: %w", commitErr)
	}

	return len(stale), nil
}

// sameEdge matches an `other` row that is the same edge as the `edges` row:
// the identity the edges table would be unique over if its NULL source did
// not exempt direct and derived rows.
const sameEdge = `other.type = edges.type AND other.source_id = edges.source_id
	AND other.target_id = edges.target_id AND other.source_path = edges.source_path
	AND other.kind = edges.kind AND other.id != edges.id`

// fallbackRow is the SQL test for a fallback row in table: a link with a
// #fragment whose target is a file, because the fragment named nothing there.
// A bare trailing `#` ([[note#]]) names the note itself, as
// wikilink.ResolveTarget reads it, so it is no fragment here either.
func fallbackRow(table string) string {
	link := `COALESCE(` + table + `.link, '')`

	return `(instr(` + link + `, '#') BETWEEN 1 AND length(` + link + `) - 1 AND instr(` + table + `.target_id, '#') = 0)`
}

// linkPlan is where one looked-up row lands in a ResolveLinks pass.
type linkPlan struct {
	edge     linkEdge
	target   string
	fallback bool // the fragment named nothing, so target is the file
}

// edgeKey is an edge's identity apart from its row and link: what makes two
// rows the same edge.
type edgeKey struct {
	edgeType   string
	sourceID   string
	sourcePath string
	kind       string
	target     string
}

func (plan linkPlan) key() edgeKey {
	return edgeKey{plan.edge.edgeType, plan.edge.sourceID, plan.edge.sourcePath, plan.edge.kind, plan.target}
}

// twinLands reports whether another row of plan's edge lands on plan's target
// this pass and outranks it there, which is what parks plan in the park step:
// a path-written row, a looked-up row that is not a fallback, or an older
// fallback row. landed indexes the plans landing on that target.
func (repo *EdgeRepo) twinLands(plan linkPlan, plans []linkPlan, landed []int) (bool, error) {
	for _, index := range landed {
		other := plans[index]

		if other.edge.rowID != plan.edge.rowID && (!other.fallback || other.edge.rowID < plan.edge.rowID) {
			return true, nil
		}
	}

	var count int

	if scanErr := repo.db.QueryRow(
		`SELECT COUNT(*) FROM edges
		 WHERE type = ? AND source_id = ? AND target_id = ? AND source_path = ? AND kind = ? AND link IS NULL`,
		plan.edge.edgeType, plan.edge.sourceID, plan.target, plan.edge.sourcePath, plan.edge.kind,
	).Scan(&count); scanErr != nil {
		return false, fmt.Errorf("edgeRepo: twin of %q from %s: %w", plan.edge.link, plan.edge.sourceID, scanErr)
	}

	return count > 0, nil
}

// RenameLink re-points the name-form edges the file at sourcePath declares
// with the link text oldName (its own, or oldName#fragment) after a rewrite of
// the file changed that text to newName: `tusk node move` rewrote [[old]] to
// [[new]], or to a full id. The rewrite re-derives only the file's own rows, so
// without this its sub-units' rows would keep resolving the old text. The new
// text is stored as the target with its fragment, and as the link while it
// still needs a lookup (wikilink.NeedsLookup): a name, or any link with a
// fragment. A full id with no fragment is a plain edge. Run ResolveLinks after.
func (repo *EdgeRepo) RenameLink(sourcePath, oldName, newName string) error {
	var nameLink any

	if wikilink.IsName(newName) {
		nameLink = newName
	}

	if _, execErr := repo.db.Exec(`
		UPDATE edges
		SET target_id = ?1 || substr(link, length(?2) + 1),
		    link      = CASE WHEN ?3 IS NULL AND link = ?2 THEN NULL ELSE ?1 || substr(link, length(?2) + 1) END
		WHERE source_path = ?4 AND (link = ?2 OR substr(link, 1, length(?2) + 1) = ?2 || '#')`,
		newName, oldName, nameLink, sourcePath,
	); execErr != nil {
		return fmt.Errorf("edgeRepo: rename link %q in %s: %w", oldName, sourcePath, execErr)
	}

	return nil
}

// ResolveLinks loads the file names under linkCase and re-points every
// looked-up edge with EdgeRepo.ResolveLinks. A nil repo is a no-op.
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
