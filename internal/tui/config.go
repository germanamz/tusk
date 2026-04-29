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
func (app *App) buildConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create config file with defaults if none exists",
		Args:  cobra.NoArgs,
		RunE:  app.runConfigInit,
	}
	initCmd.Flags().Bool("local", false, "Write ./tusk.toml instead of the global config file")

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value and write to file",
		Args:  cobra.ExactArgs(2),
		RunE:  app.runConfigSet,
	}
	setCmd.Flags().Bool("global", false, "Write to the global config (~/.config/tusk/config.toml) even when a local tusk.toml is active")

	configCmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Display current effective configuration",
			Args:  cobra.NoArgs,
			RunE:  app.runConfigShow,
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print resolved config file path",
			Args:  cobra.NoArgs,
			RunE:  app.runConfigPath,
		},
		initCmd,
		&cobra.Command{
			Use:   "get <key>",
			Short: "Get a specific config value by dot-path key",
			Args:  cobra.ExactArgs(1),
			RunE:  app.runConfigGet,
		},
		&cobra.Command{
			Use:   "validate",
			Short: "Validate config file for errors",
			Args:  cobra.NoArgs,
			RunE:  app.runConfigValidate,
		},
		&cobra.Command{
			Use:   "edit",
			Short: "Open config file in $EDITOR",
			Args:  cobra.NoArgs,
			RunE:  app.runConfigEdit,
		},
		setCmd,
	)

	return configCmd
}

func (app *App) runConfigShow(cmd *cobra.Command, _ []string) error {
	cfg, loadErr := config.Load(app.loadOpts...)

	if loadErr != nil {
		return fmt.Errorf("loading config: %w", loadErr)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	projects, projectsErr := app.projectSvc.List(ctx)

	if projectsErr != nil {
		return fmt.Errorf("listing projects: %w", projectsErr)
	}

	workflows, workflowsErr := app.workflowSvc.List(ctx)

	if workflowsErr != nil {
		return fmt.Errorf("listing workflows: %w", workflowsErr)
	}

	wfByID := make(map[uuid.UUID]*domain.Workflow, len(workflows))
	for _, workflow := range workflows {
		wfByID[workflow.ID] = workflow
	}

	out := cmd.OutOrStdout()

	if app.format == "json" {
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
		for _, project := range projects {
			payload.Projects[project.Name] = projectToConfigView(project, wfByID)
		}
		for _, workflow := range workflows {
			payload.Workflows[workflow.Name] = workflowToConfigView(workflow)
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
	data, marshalErr := toml.Marshal(wrapper)

	if marshalErr != nil {
		return fmt.Errorf("marshaling config: %w", marshalErr)
	}

	if _, err := out.Write(data); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if inline := FormatTaxonomyInline(domain.Taxonomy(cfg.Taxonomy.Levels)); inline != "" {
		if _, err := fmt.Fprintln(out, "[taxonomy]"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "levels = %q\n\n", inline); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(out, RenderWorkflowsTOML(workflows)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	_, err := fmt.Fprint(out, RenderProjectsTOML(projects, wfByID))
	return err
}

func projectToConfigView(project *domain.Project, wfByID map[uuid.UUID]*domain.Workflow) configProjectView {
	out := configProjectView{}
	if workflow, ok := wfByID[project.WorkflowID]; ok && workflow != nil {
		out.Workflow = workflow.Name
	}
	if project.Settings.AutoCompleteParent != nil {
		out.Settings.AutoCompleteParent = &configAutoView{
			TriggerStatus: project.Settings.AutoCompleteParent.TriggerStatus,
			TargetStatus:  project.Settings.AutoCompleteParent.TargetStatus,
		}
	}
	if project.Settings.AutoRevertParent != nil {
		out.Settings.AutoRevertParent = &configAutoView{
			TriggerStatus: project.Settings.AutoRevertParent.TriggerStatus,
			TargetStatus:  project.Settings.AutoRevertParent.TargetStatus,
		}
	}
	if urgency := project.Settings.Urgency; urgency != nil {
		out.Settings.Urgency = &configUrgencyView{
			PriorityWeight:    urgency.PriorityWeight,
			DueWeight:         urgency.DueWeight,
			AgeWeight:         urgency.AgeWeight,
			ActiveWeight:      urgency.ActiveWeight,
			BlockingWeight:    urgency.BlockingWeight,
			BlockedWeight:     urgency.BlockedWeight,
			TagsWeight:        urgency.TagsWeight,
			ProjectWeight:     urgency.ProjectWeight,
			AnnotationsWeight: urgency.AnnotationsWeight,
			WaitingWeight:     urgency.WaitingWeight,
		}
	}
	return out
}

func workflowToConfigView(workflow *domain.Workflow) configWorkflowView {
	out := configWorkflowView{
		Statuses:    make(map[string]configWorkflowStatusView, len(workflow.Statuses)),
		Transitions: make([]configWorkflowTransitionView, 0, len(workflow.Transitions)),
	}
	for name, statusCfg := range workflow.Statuses {
		roles := make([]string, len(statusCfg.Roles))
		for index, role := range statusCfg.Roles {
			roles[index] = string(role)
		}
		out.Statuses[name] = configWorkflowStatusView{Roles: roles}
	}
	for _, transition := range workflow.Transitions {
		out.Transitions = append(out.Transitions, configWorkflowTransitionView{
			From: transition.FromStatus,
			To:   transition.ToStatus,
		})
	}
	return out
}

func (app *App) runConfigPath(cmd *cobra.Command, _ []string) error {
	cfg, loadErr := config.Load(app.loadOpts...)

	if loadErr != nil {
		return fmt.Errorf("loading config: %w", loadErr)
	}

	if cfg.Sources.File != "" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), cfg.Sources.File)
		return err
	}

	path, pathErr := config.ConfigFilePath(app.loadOpts...)

	if pathErr != nil {
		return pathErr
	}

	if _, err := fmt.Fprintln(cmd.OutOrStdout(), path); err != nil {
		return err
	}
	_, err := fmt.Fprintln(cmd.ErrOrStderr(), "(not yet created)")
	return err
}

func (app *App) runConfigInit(cmd *cobra.Command, _ []string) error {
	if local, _ := cmd.Flags().GetBool("local"); local {
		return app.runConfigInitLocal(cmd)
	}

	path, pathErr := config.ConfigFilePath(app.loadOpts...)

	if pathErr != nil {
		return pathErr
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
	cfg, loadErr := config.Load(app.loadOpts...)

	if loadErr != nil {
		return fmt.Errorf("loading defaults: %w", loadErr)
	}

	if err := config.WriteConfig(cfg, path); err != nil {
		return err
	}

	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", path)
	return err
}

func (app *App) runConfigInitLocal(cmd *cobra.Command) error {
	cwd, cwdErr := os.Getwd()

	if cwdErr != nil {
		return fmt.Errorf("resolving working directory: %w", cwdErr)
	}

	target := filepath.Join(cwd, "tusk.toml")

	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("file exists: %s", target)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", target, err)
	}

	cfg, loadErr := config.Load(app.loadOpts...)

	if loadErr != nil {
		return fmt.Errorf("loading config: %w", loadErr)
	}

	if err := config.WriteConfig(cfg, target); err != nil {
		return err
	}

	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", target)
	return err
}

// resolveConfigWritePath picks the file `config set` should write to.
// When global is true, walk-up and any explicit file are bypassed and the
// global config path is returned (creating the file from defaults if it
// does not yet exist). When global is false, the path matches whatever
// Load() would read — typically the walk-up hit or the global file. An
// error is returned when no file exists yet and no --global was requested.
func (app *App) resolveConfigWritePath(global bool) (string, error) {
	if global {
		opts := stripLocalOpts()
		path, pathErr := config.ConfigFilePath(opts...)

		if pathErr != nil {
			return "", pathErr
		}

		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			if err := ensureGlobalConfigFile(path, opts); err != nil {
				return "", err
			}
		}
		return path, nil
	}

	path, pathErr := config.ConfigFilePath(app.loadOpts...)

	if pathErr != nil {
		return "", pathErr
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

	cfg, loadErr := config.Load(opts...)

	if loadErr != nil {
		return fmt.Errorf("loading defaults: %w", loadErr)
	}

	return config.WriteConfig(cfg, path)
}

func (app *App) runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	if strings.HasPrefix(key, "projects.") || strings.HasPrefix(key, "workflows.") {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		val, dbErr := app.configGetFromDB(ctx, key)

		if dbErr != nil {
			return dbErr
		}

		return app.writeConfigGetValue(cmd, val)
	}

	if !config.IsValidKey(key) {
		return fmt.Errorf("unknown config key: %q", key)
	}

	if key == "taxonomy.levels" {
		cfg, loadErr := config.Load(app.loadOpts...)

		if loadErr != nil {
			return fmt.Errorf("loading config: %w", loadErr)
		}

		return app.writeConfigGetValue(cmd, FormatTaxonomyInline(domain.Taxonomy(cfg.Taxonomy.Levels)))
	}

	// Build a Viper instance with the same config as Load() to get dot-path resolution.
	viperInst, viperErr := app.buildConfigViper()

	if viperErr != nil {
		return viperErr
	}

	val := viperInst.Get(key)
	if val == nil {
		return fmt.Errorf("unknown config key: %q", key)
	}

	return app.writeConfigGetValue(cmd, val)
}

// writeConfigGetValue formats a scalar or complex value to the command output
// using the same rules as the original runConfigGet body.
func (app *App) writeConfigGetValue(cmd *cobra.Command, val any) error {
	switch typed := val.(type) {
	case nil:
		if app.format == "json" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			return enc.Encode(nil)
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "")
		return err
	case string, bool, int, int64, float64:
		if app.format == "json" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			return enc.Encode(val)
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), typed)
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
func (app *App) configGetFromDB(ctx context.Context, key string) (any, error) {
	parts := strings.Split(key, ".")
	unknown := func() (any, error) { return nil, fmt.Errorf("unknown config key: %q", key) }

	switch parts[0] {
	case "projects":
		if len(parts) < 2 {
			return unknown()
		}
		name := parts[1]
		project, projectErr := app.projectSvc.GetByName(ctx, name)

		if projectErr != nil {
			if errors.Is(projectErr, domain.ErrNotFound) {
				return unknown()
			}
			return nil, projectErr
		}

		workflows, workflowsErr := app.workflowSvc.List(ctx)

		if workflowsErr != nil {
			return nil, workflowsErr
		}

		wfByID := make(map[uuid.UUID]*domain.Workflow, len(workflows))
		for _, workflow := range workflows {
			wfByID[workflow.ID] = workflow
		}
		if len(parts) == 2 {
			return projectToConfigView(project, wfByID), nil
		}
		return resolveProjectLeaf(project, wfByID, parts[2:], unknown)

	case "workflows":
		if len(parts) < 2 {
			return unknown()
		}
		name := parts[1]
		workflow, workflowErr := app.workflowSvc.GetByName(ctx, name)

		if workflowErr != nil {
			if errors.Is(workflowErr, domain.ErrNotFound) {
				return unknown()
			}
			return nil, workflowErr
		}

		if len(parts) == 2 {
			return workflowToConfigView(workflow), nil
		}
		return resolveWorkflowLeaf(workflow, parts[2:], unknown)
	}

	return unknown()
}

func resolveProjectLeaf(project *domain.Project, wfByID map[uuid.UUID]*domain.Workflow, parts []string, unknown func() (any, error)) (any, error) {
	switch parts[0] {
	case "workflow":
		if len(parts) != 1 {
			return unknown()
		}
		if workflow, ok := wfByID[project.WorkflowID]; ok && workflow != nil {
			return workflow.Name, nil
		}
		return "", nil
	case "settings":
		if len(parts) == 1 {
			return projectToConfigView(project, wfByID).Settings, nil
		}
		return resolveProjectSettingsLeaf(project, parts[1:], unknown)
	}
	return unknown()
}

func resolveProjectSettingsLeaf(project *domain.Project, parts []string, unknown func() (any, error)) (any, error) {
	switch parts[0] {
	case "auto_complete_parent":
		return resolveAutoLeaf(project.Settings.AutoCompleteParent, parts[1:], unknown)
	case "auto_revert_parent":
		ar := project.Settings.AutoRevertParent
		if ar == nil {
			return resolveAutoLeaf(nil, parts[1:], unknown)
		}
		return resolveAutoLeaf(&domain.AutoCompleteConfig{TriggerStatus: ar.TriggerStatus, TargetStatus: ar.TargetStatus}, parts[1:], unknown)
	case "urgency":
		return resolveUrgencyLeaf(project.Settings.Urgency, parts[1:], unknown)
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

func resolveUrgencyLeaf(urgency *domain.UrgencyOverrides, parts []string, unknown func() (any, error)) (any, error) {
	if len(parts) == 0 {
		if urgency == nil {
			return nil, nil
		}
		return (&configUrgencyView{
			PriorityWeight:    urgency.PriorityWeight,
			DueWeight:         urgency.DueWeight,
			AgeWeight:         urgency.AgeWeight,
			ActiveWeight:      urgency.ActiveWeight,
			BlockingWeight:    urgency.BlockingWeight,
			BlockedWeight:     urgency.BlockedWeight,
			TagsWeight:        urgency.TagsWeight,
			ProjectWeight:     urgency.ProjectWeight,
			AnnotationsWeight: urgency.AnnotationsWeight,
			WaitingWeight:     urgency.WaitingWeight,
		}), nil
	}
	if len(parts) != 1 {
		return unknown()
	}
	var fieldPtr *float64
	switch parts[0] {
	case "priority_weight":
		if urgency != nil {
			fieldPtr = urgency.PriorityWeight
		}
	case "due_weight":
		if urgency != nil {
			fieldPtr = urgency.DueWeight
		}
	case "age_weight":
		if urgency != nil {
			fieldPtr = urgency.AgeWeight
		}
	case "active_weight":
		if urgency != nil {
			fieldPtr = urgency.ActiveWeight
		}
	case "blocking_weight":
		if urgency != nil {
			fieldPtr = urgency.BlockingWeight
		}
	case "blocked_weight":
		if urgency != nil {
			fieldPtr = urgency.BlockedWeight
		}
	case "tags_weight":
		if urgency != nil {
			fieldPtr = urgency.TagsWeight
		}
	case "project_weight":
		if urgency != nil {
			fieldPtr = urgency.ProjectWeight
		}
	case "annotations_weight":
		if urgency != nil {
			fieldPtr = urgency.AnnotationsWeight
		}
	case "waiting_weight":
		if urgency != nil {
			fieldPtr = urgency.WaitingWeight
		}
	default:
		return unknown()
	}
	if fieldPtr == nil {
		return nil, nil
	}
	return *fieldPtr, nil
}

func resolveWorkflowLeaf(workflow *domain.Workflow, parts []string, unknown func() (any, error)) (any, error) {
	switch parts[0] {
	case "statuses":
		if len(parts) == 1 {
			return workflowToConfigView(workflow).Statuses, nil
		}
		statusCfg, ok := workflow.Statuses[parts[1]]
		if !ok {
			return unknown()
		}
		if len(parts) == 2 {
			return configWorkflowStatusView{Roles: rolesToStrings(statusCfg.Roles)}, nil
		}
		if len(parts) == 3 && parts[2] == "roles" {
			return rolesToStrings(statusCfg.Roles), nil
		}
		return unknown()
	case "transitions":
		if len(parts) != 1 {
			return unknown()
		}
		return workflowToConfigView(workflow).Transitions, nil
	}
	return unknown()
}

// buildConfigViper creates a Viper instance mirroring the Load() setup for dot-path access.
func (app *App) buildConfigViper() (*viper.Viper, error) {
	cfg, loadErr := config.Load(app.loadOpts...)

	if loadErr != nil {
		return nil, fmt.Errorf("loading config: %w", loadErr)
	}

	data, marshalErr := toml.Marshal(cfg)

	if marshalErr != nil {
		return nil, fmt.Errorf("marshaling config: %w", marshalErr)
	}

	viperInst := viper.New()
	viperInst.SetConfigType("toml")
	if err := viperInst.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("reading config into viper: %w", err)
	}

	return viperInst, nil
}

func (app *App) runConfigValidate(cmd *cobra.Command, _ []string) error {
	cfg, loadErr := config.Load(app.loadOpts...)

	if loadErr != nil {
		return fmt.Errorf("loading config: %w", loadErr)
	}

	if cfg.Sources.File == "" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "no user config — defaults only")
		return err
	}

	fileCfg, fileErr := config.LoadFile(cfg.Sources.File)

	if fileErr != nil {
		return fileErr
	}

	if err := fileCfg.Validate(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(cmd.OutOrStdout(), "Config valid")
	return err
}

func (app *App) runConfigEdit(cmd *cobra.Command, _ []string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("$EDITOR is not set")
	}

	cfg, loadErr := config.Load(app.loadOpts...)

	if loadErr != nil {
		return fmt.Errorf("loading config: %w", loadErr)
	}

	path := cfg.Sources.File
	if path == "" {
		initPath, pathErr := config.ConfigFilePath(app.loadOpts...)

		if pathErr != nil {
			return pathErr
		}

		if err := os.MkdirAll(filepath.Dir(initPath), 0o755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}
		if err := config.WriteConfig(cfg, initPath); err != nil {
			return err
		}
		path = initPath
	}

	editorCmd := exec.Command(editor, path)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	return editorCmd.Run()
}

func (app *App) runConfigSet(cmd *cobra.Command, args []string) error {
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
	path, pathErr := app.resolveConfigWritePath(global)

	if pathErr != nil {
		return pathErr
	}

	// taxonomy.levels uses its own inline grammar — bypass the generic
	// comma-split slice writer and parse via ParseTaxonomyInline instead.
	if key == "taxonomy.levels" {
		return setTaxonomyLevelsInline(path, value)
	}

	// Load the file contents (no defaults, no env).
	fileCfg, fileErr := config.LoadFile(path)

	if fileErr != nil {
		return fileErr
	}

	// Marshal to TOML, load into Viper for dot-path Set().
	data, marshalErr := toml.Marshal(fileCfg)

	if marshalErr != nil {
		return fmt.Errorf("marshaling config: %w", marshalErr)
	}

	viperInst := viper.New()
	viperInst.SetConfigType("toml")
	if err := viperInst.ReadConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("reading config into viper: %w", err)
	}

	// Determine if this is a slice field and parse accordingly.
	var parsedValue any
	if config.IsSliceKey(key) {
		parsedValue = strings.Split(value, ",")
	} else {
		parsedValue = value
	}

	viperInst.Set(key, parsedValue)

	// Unmarshal back to Config.
	var newCfg config.Config
	if err := viperInst.Unmarshal(&newCfg); err != nil {
		return fmt.Errorf("applying config change: %w", err)
	}

	// Validate before writing.
	if err := newCfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	return config.WriteConfig(&newCfg, path)
}

// setTaxonomyLevelsInline persists `taxonomy.levels=<inline>` to the TOML
// file at path. An empty value clears the taxonomy section (inherit
// embedded defaults / disable taxonomy). A non-empty value is parsed as
// inline syntax, validated, and written.
func setTaxonomyLevelsInline(path, value string) error {
	fileCfg, fileErr := config.LoadFile(path)

	if fileErr != nil {
		return fileErr
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		fileCfg.Taxonomy.Levels = nil
	} else {
		tax, parseErr := ParseTaxonomyInline(trimmed)

		if parseErr != nil {
			return parseErr
		}

		if err := tax.Validate(); err != nil {
			return fmt.Errorf("invalid taxonomy: %w", err)
		}
		fileCfg.Taxonomy.Levels = [][]string(tax)
	}

	if err := fileCfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return config.WriteConfig(fileCfg, path)
}
