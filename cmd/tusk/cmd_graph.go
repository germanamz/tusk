package main

import (
	"fmt"

	"github.com/germanamz/tusk/internal/graphview"
	"github.com/spf13/cobra"
)

// newGraphCmd is the deprecated alias for `tusk web`. The graph and book views
// were merged into one app; `tusk graph` now prints a deprecation notice and
// launches the unified app on the graph view, keeping its historical loopback
// port so existing scripts and muscle memory keep working. It is hidden from
// help and the generated docs.
func newGraphCmd() *cobra.Command {
	var (
		addr     string
		autoOpen bool
	)

	graphCmd := &cobra.Command{
		Use:    "graph",
		Short:  `Deprecated: use "tusk web" (opens the graph view)`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), `warning: "tusk graph" is deprecated; use "tusk web". Launching the unified app on the graph view.`)

			return runWeb(cmd, addr, autoOpen, "graph")
		},
	}

	graphCmd.Flags().StringVar(&addr, "addr", graphview.DefaultAddr, "loopback listen address")
	graphCmd.Flags().BoolVar(&autoOpen, "open", false, "open the browser automatically at startup")

	return graphCmd
}
