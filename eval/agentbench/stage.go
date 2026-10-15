package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Stage holds the two prepared trees every run copies from. Vault is the
// corpus plus tusk.toml and a built .tusk index; Corpus is the same files
// with no tusk in sight, so the grep arm can't stumble on the index.
type Stage struct {
	Vault  string
	Corpus string
}

// BuildStage copies paths (files or directories, relative to vaultRoot)
// into dir/vault and dir/corpus, then indexes dir/vault with tuskBin under
// vaultRoot's tusk.toml (see StageVault).
func BuildStage(ctx context.Context, dir string, vaultRoot string, paths []string, tuskBin string) (Stage, error) {
	stage := Stage{Vault: filepath.Join(dir, "vault"), Corpus: filepath.Join(dir, "corpus")}

	for _, rel := range paths {
		if copyErr := copyPath(filepath.Join(vaultRoot, rel), filepath.Join(stage.Corpus, rel)); copyErr != nil {
			return stage, copyErr
		}
	}

	vaultErr := StageVault(ctx, stage.Vault, vaultRoot, paths, filepath.Join(vaultRoot, "tusk.toml"), tuskBin)

	return stage, vaultErr
}

// StageVault copies paths (relative to vaultRoot) and the manifest at
// manifestPath into dir, then indexes dir with tuskBin. It fails if semantic
// search returns nothing afterwards, since a tusk arm without embeddings
// would be measuring a broken setup.
func StageVault(ctx context.Context, dir string, vaultRoot string, paths []string, manifestPath string, tuskBin string) error {
	for _, rel := range paths {
		if copyErr := copyPath(filepath.Join(vaultRoot, rel), filepath.Join(dir, rel)); copyErr != nil {
			return copyErr
		}
	}

	if manifestErr := copyPath(manifestPath, filepath.Join(dir, "tusk.toml")); manifestErr != nil {
		return fmt.Errorf("the vault needs a tusk.toml: %w", manifestErr)
	}

	if _, reindexErr := runTusk(ctx, tuskBin, dir, "reindex"); reindexErr != nil {
		return reindexErr
	}

	output, queryErr := runTusk(ctx, tuskBin, dir, "query", "", "--semantic", "overview", "--take", "1", "--json")

	if queryErr != nil {
		return queryErr
	}

	var hits []struct {
		Score float64 `json:"score"`
	}

	if decodeErr := json.Unmarshal(output, &hits); decodeErr != nil || len(hits) == 0 || hits[0].Score == 0 {
		return errors.New("a semantic query on the staged vault returned no scored hits; is Ollama running with the vault's embedding model?")
	}

	return nil
}

// TuskVersion names the binary under test: its version string plus the
// commit it was built from, since a local build reports only a dev version.
func TuskVersion(ctx context.Context, tuskBin string) string {
	version := "unknown"

	if output, versionErr := runTusk(ctx, tuskBin, "", "--version"); versionErr == nil {
		version = strings.TrimPrefix(strings.TrimSpace(string(output)), "tusk version ")
	}

	info, infoErr := buildinfo.ReadFile(tuskBin)

	if infoErr != nil {
		return version
	}

	revision, modified := "", false

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	if len(revision) > 7 {
		revision = revision[:7]
	}

	switch {
	case revision == "":
		return version
	case modified:
		return fmt.Sprintf("%s (%s, modified)", version, revision)
	default:
		return fmt.Sprintf("%s (%s)", version, revision)
	}
}

func runTusk(ctx context.Context, tuskBin string, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, tuskBin, args...)
	command.Dir = dir

	var stderr bytes.Buffer

	command.Stderr = &stderr

	output, runErr := command.Output()

	if runErr != nil {
		return nil, fmt.Errorf("tusk %s: %w\n%s", strings.Join(args, " "), runErr, stderr.String())
	}

	return output, nil
}

// tuskServerEnv runs tusk with no background workers. The staged index is
// complete before any run starts, so the drainers and watcher would only
// compete for CPU with the concurrent runs; queries are unaffected because
// the query embedding is computed inline.
func tuskServerEnv() []string {
	return append(os.Environ(), "TUSK_EMBED_WORKERS=0")
}

// WriteMCPConfig writes the --mcp-config file that starts tuskBin as the
// tusk server.
func WriteMCPConfig(path string, tuskBin string) error {
	config := map[string]any{
		"mcpServers": map[string]any{
			tuskServer: map[string]any{
				"command": tuskBin,
				"args":    []string{"mcp"},
				"env":     map[string]string{"TUSK_EMBED_WORKERS": "0"},
			},
		},
	}

	encoded, encodeErr := json.MarshalIndent(config, "", "  ")

	if encodeErr != nil {
		return encodeErr
	}

	return os.WriteFile(path, encoded, 0o600)
}

// copyPath copies a file or a directory tree, keeping modification times so
// a copied vault's index still matches its files.
func copyPath(src string, dst string) error {
	info, statErr := os.Stat(src)

	if statErr != nil {
		return statErr
	}

	if !info.IsDir() {
		return copyFile(src, dst, info)
	}

	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(src, path)

		if relErr != nil {
			return relErr
		}

		target := filepath.Join(dst, rel)

		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		if !entry.Type().IsRegular() {
			return nil
		}

		entryInfo, infoErr := entry.Info()

		if infoErr != nil {
			return infoErr
		}

		return copyFile(path, target, entryInfo)
	})
}

func copyFile(src string, dst string, info fs.FileInfo) error {
	if mkdirErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkdirErr != nil {
		return mkdirErr
	}

	source, openErr := os.Open(src)

	if openErr != nil {
		return openErr
	}

	defer source.Close()

	target, createErr := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())

	if createErr != nil {
		return createErr
	}

	if _, copyErr := io.Copy(target, source); copyErr != nil {
		target.Close()

		return copyErr
	}

	if closeErr := target.Close(); closeErr != nil {
		return closeErr
	}

	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
