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
// cancel so background work unwinds.
func runConsole(ctx context.Context, cancel context.CancelFunc, cmd *cobra.Command, viewServer *graphview.Server, rt *mcp.Runtime, url string) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "tusk graph — serving on %s\n", url)

	status := func() { printStatus(out, viewServer, rt) }

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// No TTY (CI/pipe): print status once and idle until ctx is cancelled.
		status()
		<-ctx.Done()
		_, _ = fmt.Fprintln(out)

		return
	}

	oldState, makeRawErr := term.MakeRaw(fd)
	if makeRawErr != nil {
		status()
		<-ctx.Done()
		_, _ = fmt.Fprintln(out)

		return
	}

	defer func() { _ = term.Restore(fd, oldState) }()

	keys := readKeys(ctx)

	consoleLoop(ctx, cancel, out, keys, status, func() { _ = openBrowser(url) })
}

// consoleLoop runs the status+keypress loop until ctx is done or a quit key
// (q/Q/Ctrl-C byte 3) arrives. On a quit key it calls cancel() so background
// goroutines unwind, then returns. Space invokes openURL. Pure and testable:
// the keys channel and side effects are injected.
func consoleLoop(ctx context.Context, cancel context.CancelFunc, out io.Writer, keys <-chan rune, status func(), openURL func()) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		status()

		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(out)

			return
		case key, ok := <-keys:
			if !ok {
				<-ctx.Done()
				_, _ = fmt.Fprintln(out)

				return
			}

			switch key {
			case ' ':
				openURL()
			case 'q', 'Q', 3: // 3 = Ctrl-C in raw mode (ISIG disabled)
				cancel()
				_, _ = fmt.Fprintln(out)

				return
			}
		case <-ticker.C:
		}
	}
}

func printStatus(out interface{ Write([]byte) (int, error) }, viewServer *graphview.Server, rt *mcp.Runtime) {
	gen := "?"
	if value, err := rt.Meta.Get("reindex_gen"); err == nil && value != "" {
		gen = value
	}

	nodeCount, _ := rt.Nodes.CountFileNodes()
	edges, _ := rt.Edges.ListAll()

	_, _ = fmt.Fprintf(out, "\r\033[Kindex gen %s · %d nodes · %d edges · %d clients   [space] open  [q] quit", gen, nodeCount, len(edges), viewServer.ClientCount())
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
