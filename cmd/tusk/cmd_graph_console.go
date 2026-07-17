package main

import (
	"fmt"

	"github.com/germanamz/tusk/internal/mcp"
)

// statusLine builds the graph console's one-line status text from live counts
// and the current reindex-walk state. It satisfies webUIConfig.StatusLine.
func statusLine(rt *mcp.Runtime, viewServer webViewServer) string {
	nodeCount, _ := rt.Nodes.CountFileNodes()
	edges, _ := rt.Edges.ListAll()

	return formatStatus(rt.WalkStatus.Snapshot(), nodeCount, len(edges), viewServer.ClientCount())
}

// formatStatus renders the graph console's one-line status footer from the
// current reindex-walk state and live counts. It deliberately does NOT surface
// the raw reindex generation counter: that counter bumps on every walk —
// including walks that change nothing — so a quiet, fully-indexed workspace
// leaves it frozen at some N, which readers mistake for N stuck pending items
// (the bug report this fixes was filed for exactly that). Instead it names the
// state — synced / indexing… / walk error — and, once a walk has completed,
// summarizes it (duration + nodes changed) so "idle and synced" is visibly
// distinct from "a walk is running" or "the last walk failed".
func formatStatus(snap mcp.WalkStatusSnapshot, nodeCount, edgeCount, clientCount int) string {
	counts := fmt.Sprintf("%d nodes · %d edges · %d clients", nodeCount, edgeCount, clientCount)

	const keys = "[space] open  [q] quit"

	switch {
	case snap.Walking:
		return fmt.Sprintf("indexing… · %s   %s", counts, keys)
	case snap.Last.Err != "":
		return fmt.Sprintf("walk error · %s   %s", counts, keys)
	case snap.EverWalked:
		return fmt.Sprintf("synced · %s · last walk %dms (%d changed)   %s",
			counts, snap.Last.DurationMs, snap.Last.Changed(), keys)
	default:
		return fmt.Sprintf("synced · %s   %s", counts, keys)
	}
}
