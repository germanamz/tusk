package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// buildConfigCmd creates the `tusk config` command group.
func (a *App) buildConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create config file with defaults if none exists",
		Args:  cobra.NoArgs,
		RunE:  a.runConfigInit,
	}
	initCmd.Flags().Bool("local", false, "Write ./tusk.toml instead of the global config file")

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value and write to file",
		Args:  cobra.ExactArgs(2),
		RunE:  a.runConfigSet,
	}
	setCmd.Flags().Bool("global", false, "Write to the global config (~/.config/tusk/config.toml) even when a local tusk.toml is active")

	configCmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Display current effective configuration",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigShow,
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print resolved config file path",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigPath,
		},
		initCmd,
		&cobra.Command{
			Use:   "get <key>",
			Short: "Get a specific config value by dot-path key",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runConfigGet,
		},
		&cobra.Command{
			Use:   "validate",
			Short: "Validate config file for errors",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigValidate,
		},
		&cobra.Command{
			Use:   "edit",
			Short: "Open config file in $EDITOR",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigEdit,
		},
		setCmd,
	)

	return configCmd
}

func (a *App) runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	projects, err := a.projectSvc.List(ctx)
	if err != nil {
		return fmt.Errorf("listing projects: %w", err)
	}
	workflows, err := a.workflowSvc.List(ctx)
	if err != nil {
		return fmt.Errorf("listing workflows: %w", err)
	}
	wfByID := make(map[uuid.UUID]*domain.Workflow, len(workflows))
	for _, w := range workflows {
		wfByID[w.ID] = w
	}

	out := cmd.OutOrStdout()

	if a.format == "json" {
		payload := configShowJSON{
			Storage:   cfg.Storage,
			Urgency:   cfg.Urgency,
			TUI:       cfg.TUI,
			MCP:       cfg.MCP,
			Inline:    cfg.Inline,
			Events:    cfg.Events,
			Projects:  make(map[string]configProjectView, len(projects)),
			Workflows: make(map[string]configWorkflowView, len(workflows)),
		}
		for _, p := range projects {
			payload.Projects[p.Name] = projectToConfigView(p, wfByID)
		}
		for _, w := range workflows {
			payload.Workflows[w.Name] = workflowToConfigView(w)
		}
		enc := json.NewEncoder(out)
		return enc.Encode(payload)
	}

	header := "# active: "
	if cfg.Sources.File != "" {
		header += cfg.Sources.File
	} else {
		header += "defaults only"
	}
	if _, err := fmt.Fprintln(out, header); err != nil {
		return err
	}

	wrapper := configShowTOML{
		Storage: cfg.Storage,
		Urgency: cfg.Urgency,
		TUI:     cfg.TUI,
		MCP:     cfg.MCP,
		Inline:  cfg.Inline,
		Events:  cfg.Events,
	}
	data, err := toml.Marshal(wrapper)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if _, err := out.Write(data); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if _, err := fmt.Fprint(out, RenderWorkflowsTOML(workflows)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	_, err = fmt.Fprint(out, RenderProjectsTOML(projects, wfByID))
	return err
}

func projectToConfigView(p *domain.Project, wfByID map[uuid.UUID]*domain.Workflow) configProjectView {
	out := configProjectView{}
	if wf, ok := wfByID[p.WorkflowID]; ok && wf != nil {
		out.Workflow = wf.Name
	}
	if p.Settings.AutoCompleteParent != nil {
		out.Settings.AutoCompleteParent = &configAutoView{
			TriggerStatus: p.Settings.AutoCompleteParent.TriggerStatus,
			TargetStatus:  p.Settings.AutoCompleteParent.TargetStatus,
		}
	}
	if p.Settings.AutoRevertParent != nil {
		out.Settings.AutoRevertParent = &configAutoView{
			TriggerStatus: p.Settings.AutoRevertParent.TriggerStatus,
			TargetStatus:  p.Settings.AutoRevertParent.TargetStatus,
		}
	}
	if u := p.Settings.Urgency; u != nil {
		out.Settings.Urgency = &configUrgencyView{
			PriorityWeight:    u.PriorityWeight,
			DueWeight:         u.DueWeight,
			AgeWeight:         u.AgeWeight,
			ActiveWeight:      u.ActiveWeight,
			BlockingWeight:    u.BlockingWeight,
			BlockedWeight:     u.BlockedWeight,
			TagsWeight:        u.TagsWeight,
			ProjectWeight:     u.ProjectWeight,
			AnnotationsWeight: u.AnnotationsWeight,
			WaitingWeight:     u.WaitingWeight,
		}
	}
	return out
}

func workflowToConfigView(w *domain.Workflow) configWorkflowView {
	out := configWorkflowView{
		Statuses:    make(map[string]configWorkflowStatusView, len(w.Statuses)),
		Transitions: make([]configWorkflowTransitionView, 0, len(w.Transitions)),
	}
	for name, sc := range w.Statuses {
		roles := make([]string, len(sc.Roles))
		for i, r := range sc.Roles {
			roles[i] = string(r)
		}
		out.Statuses[name] = configWorkflowStatusView{Roles: roles}
	}
	for _, tr := range w.Transitions {
		out.Transitions = append(out.Transitions, configWorkflowTransitionView{
			From: tr.FromStatus,
			To:   tr.ToStatus,
		})
	}
	return out
}

func (a *App) runConfigPath(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Sources.File != "" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), cfg.Sources.File)
		return err
	}

	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), path); err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.ErrOrStderr(), "(not yet created)")
	return err
}

func (a *App) runConfigInit(cmd *cobra.Command, args []string) error {
	if local, _ := cmd.Flags().GetBool("local"); local {
		return a.runConfigInitLocal(cmd)
	}

	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Config file already exists: %s\n", path)
		return err
	}

	// Create directory and write defaults.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Load embedded defaults and write them.
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading defaults: %w", err)
	}
	if err := config.WriteConfig(cfg, path); err != nil {
		return err
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", path)
	return err
}

func (a *App) runConfigInitLocal(cmd *cobra.Command) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}
	target := filepath.Join(cwd, "tusk.toml")

	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("file exists: %s", target)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", target, err)
	}

	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := config.WriteConfig(cfg, target); err != nil {
		return err
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", target)
	return err
}

// resolveConfigWritePath picks the file `config set` should write to.
// When global is true, walk-up and any explicit file are bypassed and the
// global config path is returned (creating the file from defaults if it
// does not yet exist). When global is false, the path matches whatever
// Load() would read — typically the walk-up hit or the global file. An
// error is returned when no file exists yet and no --global was requested.
func (a *App) resolveConfigWritePath(global bool) (string, error) {
	if global {
		opts := stripLocalOpts()
		path, err := config.ConfigFilePath(opts...)
		if err != nil {
			return "", err
		}
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			if err := ensureGlobalConfigFile(path, opts); err != nil {
				return "", err
			}
		}
		return path, nil
	}

	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return "", fmt.Errorf(`no config file found; run "tusk config init" or "tusk config init --local"`)
	}
	return path, nil
}

// stripLocalOpts returns a loadOpts slice that bypasses walk-up and any
// explicit-file override. Passing nil to config.ConfigFilePath falls through
// to the global branch while still honoring TUSK_CONFIG_DIR.
func stripLocalOpts() []config.Option {
	return nil
}

// ensureGlobalConfigFile writes a default config to path when the file does
// not yet exist. opts should be the stripped option set so Load pulls pure
// defaults (plus any TUSK_* env overrides) rather than inheriting walk-up.
func ensureGlobalConfigFile(path string, opts []config.Option) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	cfg, err := config.Load(opts...)
	if err != nil {
		return fmt.Errorf("loading defaults: %w", err)
	}
	return config.WriteConfig(cfg, path)
}

func (a *App) runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	if strings.HasPrefix(key, "projects.") || strings.HasPrefix(key, "workflows.") {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		val, err := a.configGetFromDB(ctx, key)
		if err != nil {
			return err
		}
		return a.writeConfigGetValue(cmd, val)
	}

	if !config.IsValidKey(key) {
		return fmt.Errorf("unknown config key: %q", key)
	}

	// Build a Viper instance with the same config as Load() to get dot-path resolution.
	v, err := a.buildConfigViper()
	if err != nil {
		return err
	}

	val := v.Get(key)
	if val == nil {
		return fmt.Errorf("unknown config key: %q", key)
	}

	return a.writeConfigGetValue(cmd, val)
}

// writeConfigGetValue formats a scalar or complex value to the command output
// using the same rules as the original runConfigGet body.
func (a *App) writeConfigGetValue(cmd *cobra.Command, val any) error {
	switch v := val.(type) {
	case nil:
		if a.format == "json" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			return enc.Encode(nil)
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "")
		return err
	case string, bool, int, int64, float64:
		if a.format == "json" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			return enc.Encode(val)
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), v)
		return err
	default:
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(val)
	}
}

// configGetFromDB resolves a projects.* / workflows.* dot-path key against the
// database-backed project and workflow services. It returns typed scalar
// values for leaf segments, structured JSON-shaped values for table segments,
// and an error for unknown keys.
func (a *App) configGetFromDB(ctx context.Context, key string) (any, error) {
	parts := strings.Split(key, ".")
	unknown := func() (any, error) { return nil, fmt.Errorf("unknown config key: %q", key) }

	switch parts[0] {
	case "projects":
		if len(parts) < 2 {
			return unknown()
		}
		name := parts[1]
		p, err := a.projectSvc.GetByName(ctx, name)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return unknown()
			}
			return nil, err
		}
		workflows, err := a.workflowSvc.List(ctx)
		if err != nil {
			return nil, err
		}
		wfByID := make(map[uuid.UUID]*domain.Workflow, len(workflows))
		for _, w := range workflows {
			wfByID[w.ID] = w
		}
		if len(parts) == 2 {
			return projectToConfigView(p, wfByID), nil
		}
		return resolveProjectLeaf(p, wfByID, parts[2:], unknown)

	case "workflows":
		if len(parts) < 2 {
			return unknown()
		}
		name := parts[1]
		w, err := a.workflowSvc.GetByName(ctx, name)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return unknown()
			}
			return nil, err
		}
		if len(parts) == 2 {
			return workflowToConfigView(w), nil
		}
		return resolveWorkflowLeaf(w, parts[2:], unknown)
	}

	return unknown()
}

func resolveProjectLeaf(p *domain.Project, wfByID map[uuid.UUID]*domain.Workflow, parts []string, unknown func() (any, error)) (any, error) {
	switch parts[0] {
	case "workflow":
		if len(parts) != 1 {
			return unknown()
		}
		if wf, ok := wfByID[p.WorkflowID]; ok && wf != nil {
			return wf.Name, nil
		}
		return "", nil
	case "settings":
		if len(parts) == 1 {
			return projectToConfigView(p, wfByID).Settings, nil
		}
		return resolveProjectSettingsLeaf(p, parts[1:], unknown)
	}
	return unknown()
}

func resolveProjectSettingsLeaf(p *domain.Project, parts []string, unknown func() (any, error)) (any, error) {
	switch parts[0] {
	case "auto_complete_parent":
		return resolveAutoLeaf(p.Settings.AutoCompleteParent, parts[1:], unknown)
	case "auto_revert_parent":
		ar := p.Settings.AutoRevertParent
		if ar == nil {
			return resolveAutoLeaf(nil, parts[1:], unknown)
		}
		return resolveAutoLeaf(&domain.AutoCompleteConfig{TriggerStatus: ar.TriggerStatus, TargetStatus: ar.TargetStatus}, parts[1:], unknown)
	case "urgency":
		return resolveUrgencyLeaf(p.Settings.Urgency, parts[1:], unknown)
	}
	return unknown()
}

func resolveAutoLeaf(ac *domain.AutoCompleteConfig, parts []string, unknown func() (any, error)) (any, error) {
	if len(parts) == 0 {
		if ac == nil {
			return nil, nil
		}
		return map[string]any{
			"trigger_status": ac.TriggerStatus,
			"target_status":  ac.TargetStatus,
		}, nil
	}
	if len(parts) != 1 {
		return unknown()
	}
	if ac == nil {
		switch parts[0] {
		case "trigger_status", "target_status":
			return nil, nil
		}
		return unknown()
	}
	switch parts[0] {
	case "trigger_status":
		return ac.TriggerStatus, nil
	case "target_status":
		return ac.TargetStatus, nil
	}
	return unknown()
}

func resolveUrgencyLeaf(u *domain.UrgencyOverrides, parts []string, unknown func() (any, error)) (any, error) {
	if len(parts) == 0 {
		if u == nil {
			return nil, nil
		}
		return (&configUrgencyView{
			PriorityWeight:    u.PriorityWeight,
			DueWeight:         u.DueWeight,
			AgeWeight:         u.AgeWeight,
			ActiveWeight:      u.ActiveWeight,
			BlockingWeight:    u.BlockingWeight,
			BlockedWeight:     u.BlockedWeight,
			TagsWeight:        u.TagsWeight,
			ProjectWeight:     u.ProjectWeight,
			AnnotationsWeight: u.AnnotationsWeight,
			WaitingWeight:     u.WaitingWeight,
		}), nil
	}
	if len(parts) != 1 {
		return unknown()
	}
	var ptr *float64
	switch parts[0] {
	case "priority_weight":
		if u != nil {
			ptr = u.PriorityWeight
		}
	case "due_weight":
		if u != nil {
			ptr = u.DueWeight
		}
	case "age_weight":
		if u != nil {
			ptr = u.AgeWeight
		}
	case "active_weight":
		if u != nil {
			ptr = u.ActiveWeight
		}
	case "blocking_weight":
		if u != nil {
			ptr = u.BlockingWeight
		}
	case "blocked_weight":
		if u != nil {
			ptr = u.BlockedWeight
		}
	case "tags_weight":
		if u != nil {
			ptr = u.TagsWeight
		}
	case "project_weight":
		if u != nil {
			ptr = u.ProjectWeight
		}
	case "annotations_weight":
		if u != nil {
			ptr = u.AnnotationsWeight
		}
	case "waiting_weight":
		if u != nil {
			ptr = u.WaitingWeight
		}
	default:
		return unknown()
	}
	if ptr == nil {
		return nil, nil
	}
	return *ptr, nil
}

func resolveWorkflowLeaf(w *domain.Workflow, parts []string, unknown func() (any, error)) (any, error) {
	switch parts[0] {
	case "statuses":
		if len(parts) == 1 {
			return workflowToConfigView(w).Statuses, nil
		}
		status, ok := w.Statuses[parts[1]]
		if !ok {
			return unknown()
		}
		if len(parts) == 2 {
			return configWorkflowStatusView{Roles: rolesToStrings(status.Roles)}, nil
		}
		if len(parts) == 3 && parts[2] == "roles" {
			return rolesToStrings(status.Roles), nil
		}
		return unknown()
	case "transitions":
		if len(parts) != 1 {
			return unknown()
		}
		return workflowToConfigView(w).Transitions, nil
	}
	return unknown()
}

// buildConfigViper creates a Viper instance mirroring the Load() setup for dot-path access.
func (a *App) buildConfigViper() (*viper.Viper, error) {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("reading config into viper: %w", err)
	}

	return v, nil
}

func (a *App) runConfigValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Sources.File == "" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "no user config — defaults only")
		return err
	}

	fileCfg, err := config.LoadFile(cfg.Sources.File)
	if err != nil {
		return err
	}
	if err := fileCfg.Validate(); err != nil {
		return err
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), "Config valid")
	return err
}

func (a *App) runConfigEdit(cmd *cobra.Command, args []string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("$EDITOR is not set")
	}

	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	path := cfg.Sources.File
	if path == "" {
		initPath, err := config.ConfigFilePath(a.loadOpts...)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(initPath), 0o755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}
		if err := config.WriteConfig(cfg, initPath); err != nil {
			return err
		}
		path = initPath
	}

	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func (a *App) runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	if strings.HasPrefix(key, "projects.") {
		return fmt.Errorf("projects.* is managed by the database — use `tusk project modify` instead")
	}
	if strings.HasPrefix(key, "workflows.") {
		return fmt.Errorf("workflows.* is managed by the database — use `tusk workflow modify` instead")
	}

	if !config.IsValidKey(key) {
		return fmt.Errorf("unknown config key: %q", key)
	}

	global, _ := cmd.Flags().GetBool("global")
	path, err := a.resolveConfigWritePath(global)
	if err != nil {
		return err
	}

	// Load the file contents (no defaults, no env).
	fileCfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}

	// Marshal to TOML, load into Viper for dot-path Set().
	data, err := toml.Marshal(fileCfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("reading config into viper: %w", err)
	}

	// Determine if this is a slice field and parse accordingly.
	var parsedValue any
	if config.IsSliceKey(key) {
		parsedValue = strings.Split(value, ",")
	} else {
		parsedValue = value
	}

	v.Set(key, parsedValue)

	// Unmarshal back to Config.
	var newCfg config.Config
	if err := v.Unmarshal(&newCfg); err != nil {
		return fmt.Errorf("applying config change: %w", err)
	}

	// Validate before writing.
	if err := newCfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	return config.WriteConfig(&newCfg, path)
}
