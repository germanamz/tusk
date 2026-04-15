package tui

import "github.com/spf13/cobra"

const completionLong = `Generate shell completion scripts for tusk.

Completion scripts are generated on demand from the current Cobra command
tree. tusk does not ship pre-baked completion artifacts, so regenerate and
reinstall after every upgrade.

# bash — user scope
tusk completion bash > ~/.local/share/bash-completion/completions/tusk

# zsh — drop in any directory listed in $fpath
tusk completion zsh > "${fpath[1]}/_tusk"

# fish — user scope
tusk completion fish > ~/.config/fish/completions/tusk.fish

# powershell — append to your profile
tusk completion powershell | Out-String | Invoke-Expression
`

// buildCompletionCmd returns the `tusk completion` parent command and its
// four shell leaves. The generators emit scripts to cmd.OutOrStdout() so
// that tests redirecting the root's output stream capture the content.
func (a *App) buildCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "completion [bash|zsh|fish|powershell]",
		Short:             "Generate shell completion scripts",
		Long:              completionLong,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:                   "bash",
		Short:                 "Generate the autocompletion script for bash",
		Args:                  cobra.NoArgs,
		DisableFlagsInUseLine: true,
		ValidArgsFunction:     cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Root().GenBashCompletionV2(cmd.OutOrStdout(), true)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:                   "zsh",
		Short:                 "Generate the autocompletion script for zsh",
		Args:                  cobra.NoArgs,
		DisableFlagsInUseLine: true,
		ValidArgsFunction:     cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:                   "fish",
		Short:                 "Generate the autocompletion script for fish",
		Args:                  cobra.NoArgs,
		DisableFlagsInUseLine: true,
		ValidArgsFunction:     cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:                   "powershell",
		Short:                 "Generate the autocompletion script for powershell",
		Args:                  cobra.NoArgs,
		DisableFlagsInUseLine: true,
		ValidArgsFunction:     cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Root().GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
		},
	})

	return cmd
}
