package main

import (
	"fmt"

	"github.com/germanamz/tusk/internal/bookview"
	"github.com/spf13/cobra"
)

// newBookCmd is the deprecated alias for `tusk web`. The graph and book views
// were merged into one app; `tusk book` now prints a deprecation notice and
// launches the unified app on the reading view, keeping its historical loopback
// port so existing scripts and muscle memory keep working. It is hidden from
// help and the generated docs.
func newBookCmd() *cobra.Command {
	var (
		addr     string
		autoOpen bool
	)

	bookCmd := &cobra.Command{
		Use:    "book",
		Short:  `Deprecated: use "tusk web" (opens the reading view)`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), `warning: "tusk book" is deprecated; use "tusk web". Launching the unified app on the reading view.`)

			return runWeb(cmd, addr, autoOpen, "read")
		},
	}

	bookCmd.Flags().StringVar(&addr, "addr", bookview.DefaultAddr, "loopback listen address")
	bookCmd.Flags().BoolVar(&autoOpen, "open", false, "open the browser automatically at startup")

	return bookCmd
}
