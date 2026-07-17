package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/germanamz/tusk/internal/mcp"
	"github.com/spf13/cobra"
)

// webViewServer is what a local web view must expose for serveWebUI to host it:
// an HTTP handler to mount, background work to run for the lifetime of the
// serve, and a live count of connected SSE clients for the status footer.
// Handler() owns its own Host-header guard — serveWebUI mounts it unwrapped.
type webViewServer interface {
	Handler() http.Handler
	Run(ctx context.Context)
	ClientCount() int
}

// webUIConfig describes one local web view for serveWebUI to serve. Name and
// Title are the two user-facing labels: Name prefixes errors the way each
// command already prefixes its own ("graph: listen …"), while Title is the
// display name in the console intro line ("tusk graph — serving on …").
type webUIConfig struct {
	Name     string
	Addr     string
	AutoOpen bool
	Title    string

	// BuildServer constructs the view server once the runtime is open.
	BuildServer func(rt *mcp.Runtime) webViewServer

	// StatusLine renders the console's one-line status from the live runtime and
	// the view server's client count.
	StatusLine func(rt *mcp.Runtime, viewServer webViewServer) string

	ready func(addr string) // optional; called once listening (tests)
}

// serveWebUI opens the runtime, starts background maintenance, serves the view's
// handler, and runs the foreground console until ctx is cancelled. It is the
// shared spine behind `tusk graph` and `tusk book`; everything view-specific
// arrives through cfg.
func serveWebUI(ctx context.Context, cmd *cobra.Command, cfg webUIConfig) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return cwdErr
	}

	// footer coordinates the interactive status footer with the background logs
	// so a -v log line never glues onto the footer. It wraps stderr (where the
	// logs already go) and stays a transparent passthrough until runConsole
	// activates it on an interactive terminal. Building one logger over it and
	// sharing it between the runtime and the view server keeps every background
	// component's output flowing through the same coordinator.
	footer := newFooterWriter(cmd.ErrOrStderr())

	verbose, _ := cmd.Flags().GetBool("verbose")
	logger := newLogger(footer, verbose)

	opts := []mcp.Option{
		mcp.WithAliasIntrospector(buildVerbIntrospector(cmd.Root())),
		mcp.WithLogger(logger),
	}

	runtime, openErr := mcp.Open(cwd, opts...)
	if openErr != nil {
		return openErr
	}

	defer runtime.Close()

	viewServer := cfg.BuildServer(runtime)

	// Background maintenance (watcher + drainers) and the SSE hub.
	bgServer := mcp.NewServer(runtime)

	bgDone := make(chan error, 1)
	go func() { bgDone <- bgServer.RunBackground(ctx) }()

	go viewServer.Run(ctx)

	listener, listenErr := net.Listen("tcp", cfg.Addr)
	if listenErr != nil {
		cancel()
		<-bgDone

		return fmt.Errorf("%s: listen %s: %w", cfg.Name, cfg.Addr, listenErr)
	}

	boundURL := "http://" + listener.Addr().String()

	if cfg.ready != nil {
		cfg.ready(listener.Addr().String())
	}

	// Route the HTTP server's own error log through the footer coordinator too,
	// so a stray net/http error line (default destination: os.Stderr) does not
	// glue onto the interactive footer either.
	httpServer := &http.Server{
		Handler:           viewServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(footer, "", 0),
	}

	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- httpServer.Serve(listener) }()

	if cfg.AutoOpen {
		_ = openBrowser(boundURL)
	}

	// Tilt-style foreground console (status line + keypress loop).
	status := func() string { return cfg.StatusLine(runtime, viewServer) }

	runConsole(ctx, cancel, cmd, cfg.Title, status, boundURL, footer)

	cancel() // unblock RunBackground + viewServer.Run before draining

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)

	<-bgDone

	if serveErr := <-serveErrCh; serveErr != nil && serveErr != http.ErrServerClosed {
		return serveErr
	}

	return nil
}

// deriveAllowedHosts derives a view server's Host-header allowlist from the
// bound address. Loopback binds stay strict (loopback Host only), which is what
// DNS-rebinding protection needs. A specific non-loopback bind — already
// confirmed by the user — allows that host so the intended access path works;
// an all-interfaces bind can't enumerate the access host, so the guard is
// disabled with "*" since the user has accepted network exposure.
func deriveAllowedHosts(addr string) []string {
	if isLoopbackAddr(addr) {
		return nil
	}

	host, _, splitErr := net.SplitHostPort(addr)

	if splitErr != nil {
		host = addr
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		return []string{"*"}
	}

	return []string{host}
}

// isLoopbackAddr reports whether addr binds only the loopback interface.
func isLoopbackAddr(addr string) bool {
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return false
	}

	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

func confirmNonLoopback(cmd *cobra.Command, addr, noun string) bool {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %q is not loopback; the %s server is unauthenticated and read-only but would be reachable from your network.\nProceed? [y/N] ", addr, noun)

	var answer string
	_, _ = fmt.Fscanln(cmd.InOrStdin(), &answer)

	return answer == "y" || answer == "Y"
}
