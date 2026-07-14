package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/germanamz/tusk/internal/graphview"
	"github.com/germanamz/tusk/internal/mcp"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// runConsole sets up raw-mode key reading (when stdin is a TTY) and drives the
// live console loop. It restores the terminal on return; on a quit key it calls
// cancel so background work unwinds. footer is the shared coordinated writer the
// background verbose logs also flow through; runConsole activates it only on an
// interactive terminal so the sticky footer and log lines never collide.
func runConsole(ctx context.Context, cancel context.CancelFunc, cmd *cobra.Command, viewServer *graphview.Server, rt *mcp.Runtime, url string, footer *footerWriter) {
	fd := int(os.Stdin.Fd())

	if !term.IsTerminal(fd) {
		// No TTY (CI/pipe): print a plain status line once and idle. The footer
		// stays passive, so background logs pass straight through to stderr.
		printPlainStatus(cmd.OutOrStdout(), viewServer, rt, url)
		<-ctx.Done()

		return
	}

	oldState, makeRawErr := term.MakeRaw(fd)
	if makeRawErr != nil {
		printPlainStatus(cmd.OutOrStdout(), viewServer, rt, url)
		<-ctx.Done()

		return
	}

	defer func() { _ = term.Restore(fd, oldState) }()

	// Interactive: route the intro line, the sticky footer, and the background
	// verbose logs through the one coordinated writer so a -v log line never glues
	// onto the footer text. finish() leaves the cursor on a fresh line for the
	// shell prompt.
	_, _ = fmt.Fprintf(footer, "tusk graph — serving on %s\n", url)
	footer.activate()

	defer footer.finish()

	status := func() { footer.setFooter(statusLine(viewServer, rt)) }
	keys := readKeys(ctx)

	consoleLoop(ctx, cancel, keys, status, func() { _ = openBrowser(url) })
}

// consoleLoop runs the status+keypress loop until ctx is done or a quit key
// (q/Q/Ctrl-C byte 3) arrives. On a quit key it calls cancel() so background
// goroutines unwind, then returns. Space invokes openURL. Pure and testable:
// the keys channel and side effects are injected. Terminal cleanup (the final
// newline) is the caller's responsibility via footerWriter.finish.
func consoleLoop(ctx context.Context, cancel context.CancelFunc, keys <-chan rune, status func(), openURL func()) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		status()

		select {
		case <-ctx.Done():
			return
		case key, ok := <-keys:
			if !ok {
				<-ctx.Done()

				return
			}

			switch key {
			case ' ':
				openURL()
			case 'q', 'Q', 3: // 3 = Ctrl-C in raw mode (ISIG disabled)
				cancel()

				return
			}
		case <-ticker.C:
		}
	}
}

// printPlainStatus writes the intro and a single newline-terminated status line
// for the non-interactive path (CI, a pipe, or a failed raw-mode setup), where
// there is no sticky footer to redraw.
func printPlainStatus(out io.Writer, viewServer *graphview.Server, rt *mcp.Runtime, url string) {
	_, _ = fmt.Fprintf(out, "tusk graph — serving on %s\n", url)
	_, _ = fmt.Fprintln(out, statusLine(viewServer, rt))
}

// statusLine builds the one-line status text from live counts and the current
// reindex-walk state.
func statusLine(viewServer *graphview.Server, rt *mcp.Runtime) string {
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

// readKeys spawns a goroutine that reads single bytes from stdin and forwards
// them as runes until ctx is cancelled or the read fails. It does NOT manage
// terminal state (the caller owns MakeRaw/Restore). The reader goroutine may
// remain parked in Read at shutdown; that is acceptable — the terminal is
// already restored by the caller's deferred Restore.
func readKeys(ctx context.Context) <-chan rune {
	out := make(chan rune)

	go func() {
		defer close(out)

		buf := make([]byte, 1)

		for {
			if ctx.Err() != nil {
				return
			}

			n, readErr := os.Stdin.Read(buf)
			if readErr != nil || n == 0 {
				return
			}

			select {
			case out <- rune(buf[0]):
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
