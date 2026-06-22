package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/germanamz/tusk/internal/graphview"
	"github.com/germanamz/tusk/internal/mcp"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// runConsole renders a live status line and, when stdin is a TTY, opens the
// browser on space and quits on q. It returns when ctx is cancelled (signal),
// or when the user presses q.
func runConsole(ctx context.Context, cmd *cobra.Command, viewServer *graphview.Server, rt *mcp.Runtime, url string) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "tusk graph — serving on %s\n", url)

	keys := watchKeys(ctx)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		printStatus(out, viewServer, rt)

		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(out)

			return
		case key, ok := <-keys:
			if !ok {
				<-ctx.Done() // no TTY: idle until signal

				return
			}

			switch key {
			case ' ':
				_ = openBrowser(url)
			case 'q', 'Q', 3: // 3 = Ctrl-C in raw mode
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

// watchKeys returns a channel of key runes read in raw mode. If stdin is not a
// TTY (CI/pipe), it returns a closed channel so the console idles until signal.
func watchKeys(ctx context.Context) <-chan rune {
	out := make(chan rune)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		close(out)

		return out
	}

	oldState, makeRawErr := term.MakeRaw(fd)
	if makeRawErr != nil {
		close(out)

		return out
	}

	go func() {
		defer func() { _ = term.Restore(fd, oldState) }()
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
