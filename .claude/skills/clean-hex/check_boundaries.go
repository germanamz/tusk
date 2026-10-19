//go:build ignore

// check_boundaries is the ratchet behind the clean-hex skill. It
// parses every non-test Go file under cmd/ and internal/ and fails when a core
// package gains a technology import, an adapter import, or a direct clock, env
// or file-system call beyond the debt frozen in boundaries.txt, or when a
// driven adapter imports a driving one.
//
// Run from the repository root:
//
//	go run .claude/skills/clean-hex/check_boundaries.go
//	go run .claude/skills/clean-hex/check_boundaries.go -dump
//
// -dump prints the debt lines that match the tree as it is now. Use it to see
// where the debt stands, never to absorb a new violation into the baseline.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const configPath = ".claude/skills/clean-hex/boundaries.txt"

// technologyImports are the imports that talk to the outside world or bind a
// package to a delivery framework. A prefix ending in "/" matches every
// package below it.
var technologyImports = []string{
	"database/sql",
	"database/sql/driver",
	"modernc.org/sqlite",
	"net",
	"net/http",
	"os/exec",
	"os/signal",
	"math/rand",
	"math/rand/v2",
	"crypto/rand",
	"github.com/google/uuid",
	"github.com/spf13/cobra",
	"github.com/spf13/cobra/",
	"github.com/spf13/pflag",
	"github.com/mark3labs/mcp-go/",
	"github.com/fsnotify/fsnotify",
	"github.com/gofrs/flock",
	"github.com/mattn/go-isatty",
	"golang.org/x/term",
}

// forbiddenCalls groups the stdlib calls a core package reaches the clock, the
// environment or the file system through, keyed by import path then function.
var forbiddenCalls = map[string]map[string]string{
	"time": {"Now": "clock", "Since": "clock", "Until": "clock"},
	"os": {
		"Getenv": "env", "LookupEnv": "env", "Environ": "env",
		"ReadFile": "fs", "WriteFile": "fs", "Open": "fs", "OpenFile": "fs",
		"Create": "fs", "CreateTemp": "fs", "Stat": "fs", "Lstat": "fs",
		"ReadDir": "fs", "Rename": "fs", "Remove": "fs", "RemoveAll": "fs",
		"Mkdir": "fs", "MkdirAll": "fs", "MkdirTemp": "fs", "Chmod": "fs",
		"Chtimes": "fs", "Symlink": "fs", "Readlink": "fs", "Getwd": "fs",
		"DirFS": "fs",
	},
	"path/filepath": {
		"Walk": "fs", "WalkDir": "fs", "Glob": "fs", "EvalSymlinks": "fs", "Abs": "fs",
	},
}

type role string

const (
	roleCore    role = "core"
	roleDriving role = "driving"
	roleDriven  role = "driven"
	roleRoot    role = "root"
)

// debtKey names one kind of violation in one package.
type debtKey struct {
	pkg  string
	item string
}

// debtAllowance is a frozen count, or any count when wildcard is set.
type debtAllowance struct {
	count    int
	wildcard bool
	line     int
}

type config struct {
	roles map[string]role
	debts map[debtKey]debtAllowance
}

func main() {
	dump := flag.Bool("dump", false, "print the debt lines matching the current tree and exit")
	flag.Parse()

	module, moduleErr := readModulePath("go.mod")

	if moduleErr != nil {
		fail("read go.mod (run from the repository root): %v", moduleErr)
	}

	cfg, configErr := readConfig(configPath)

	if configErr != nil {
		fail("read %s: %v", configPath, configErr)
	}

	files, scanErr := scanPackages()

	if scanErr != nil {
		fail("scan packages: %v", scanErr)
	}

	found := tally(files, cfg, module)

	if *dump {
		printDump(found)

		return
	}

	os.Exit(report(found, cfg, files))
}

// sourceFile is one parsed non-test file and the package it belongs to.
type sourceFile struct {
	pkg    string
	path   string
	fset   *token.FileSet
	syntax *ast.File
}

func readModulePath(path string) (string, error) {
	data, err := os.ReadFile(path)

	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}

	return "", fmt.Errorf("no module line")
}

func readConfig(path string) (config, error) {
	cfg := config{roles: map[string]role{}, debts: map[debtKey]debtAllowance{}}

	handle, openErr := os.Open(path)

	if openErr != nil {
		return cfg, openErr
	}

	defer handle.Close()

	scanner := bufio.NewScanner(handle)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		fields := strings.Fields(scanner.Text())

		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}

		switch {
		case fields[0] == "role" && len(fields) == 3:
			assigned := role(fields[1])

			if assigned != roleRoot && assigned != roleDriving && assigned != roleDriven {
				return cfg, fmt.Errorf("line %d: unknown role %q", lineNo, fields[1])
			}

			cfg.roles[fields[2]] = assigned
		case fields[0] == "debt" && len(fields) == 4:
			allowance := debtAllowance{line: lineNo}

			if fields[3] == "*" {
				allowance.wildcard = true
			} else {
				count, atoiErr := strconv.Atoi(fields[3])

				if atoiErr != nil {
					return cfg, fmt.Errorf("line %d: bad count %q", lineNo, fields[3])
				}

				allowance.count = count
			}

			cfg.debts[debtKey{pkg: fields[1], item: fields[2]}] = allowance
		default:
			return cfg, fmt.Errorf("line %d: want `role <core|driving|driven|root> <pkg>` or `debt <pkg> <item> <count|*>`", lineNo)
		}
	}

	return cfg, scanner.Err()
}

func scanPackages() ([]sourceFile, error) {
	var files []sourceFile

	for _, top := range []string{"cmd", "internal"} {
		walkErr := filepath.WalkDir(top, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			name := entry.Name()

			if entry.IsDir() {
				if name == "testdata" || name == "dist" || name == "node_modules" ||
					(path != top && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_"))) {
					return filepath.SkipDir
				}

				return nil
			}

			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}

			fset := token.NewFileSet()
			parsed, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)

			if parseErr != nil {
				return parseErr
			}

			files = append(files, sourceFile{pkg: filepath.ToSlash(filepath.Dir(path)), path: path, fset: fset, syntax: parsed})

			return nil
		})

		if walkErr != nil {
			return nil, walkErr
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no Go files under cmd/ or internal/")
	}

	return files, nil
}

func roleOf(cfg config, pkg string) role {
	if assigned, ok := cfg.roles[pkg]; ok {
		return assigned
	}

	return roleCore
}

func isTechnology(importPath string) bool {
	for _, tech := range technologyImports {
		if importPath == tech || (strings.HasSuffix(tech, "/") && strings.HasPrefix(importPath, tech)) {
			return true
		}
	}

	return false
}

// tally records every boundary-relevant occurrence, keyed by package and item.
func tally(files []sourceFile, cfg config, module string) map[debtKey][]string {
	found := map[debtKey][]string{}

	for _, file := range files {
		ownRole := roleOf(cfg, file.pkg)

		if ownRole == roleRoot || ownRole == roleDriving {
			continue
		}

		locals := map[string]string{}

		for _, spec := range file.syntax.Imports {
			importPath, _ := strconv.Unquote(spec.Path.Value)
			where := file.fset.Position(spec.Pos()).String()

			if internal, ok := strings.CutPrefix(importPath, module+"/"); ok {
				target := roleOf(cfg, internal)
				flagged := (ownRole == roleCore && target != roleCore) ||
					(ownRole == roleDriven && (target == roleDriving || target == roleRoot))

				if flagged {
					key := debtKey{pkg: file.pkg, item: "import:" + internal}
					found[key] = append(found[key], where)
				}

				continue
			}

			if ownRole == roleCore && isTechnology(importPath) {
				key := debtKey{pkg: file.pkg, item: "import:" + importPath}
				found[key] = append(found[key], where)
			}

			if _, watched := forbiddenCalls[importPath]; watched {
				local := importPath[strings.LastIndex(importPath, "/")+1:]

				if spec.Name != nil {
					local = spec.Name.Name
				}

				locals[local] = importPath
			}
		}

		if ownRole != roleCore {
			continue
		}

		ast.Inspect(file.syntax, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)

			if !ok {
				return true
			}

			selector, ok := call.Fun.(*ast.SelectorExpr)

			if !ok {
				return true
			}

			ident, ok := selector.X.(*ast.Ident)

			if !ok {
				return true
			}

			importPath, imported := locals[ident.Name]

			if !imported {
				return true
			}

			if group, forbidden := forbiddenCalls[importPath][selector.Sel.Name]; forbidden {
				key := debtKey{pkg: file.pkg, item: group}
				where := file.fset.Position(call.Pos()).String()
				found[key] = append(found[key], where+" "+ident.Name+"."+selector.Sel.Name)
			}

			return true
		})
	}

	return found
}

// isPackageWide reports whether an item is frozen per package rather than
// per occurrence: imports of tusk's own adapter packages.
func isPackageWide(item string) bool {
	return strings.HasPrefix(item, "import:") && !isTechnology(strings.TrimPrefix(item, "import:"))
}

func sortedKeys[Value any](entries map[debtKey]Value) []debtKey {
	keys := make([]debtKey, 0, len(entries))

	for key := range entries {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(left, right int) bool {
		if keys[left].pkg != keys[right].pkg {
			return keys[left].pkg < keys[right].pkg
		}

		return keys[left].item < keys[right].item
	})

	return keys
}

func printDump(found map[debtKey][]string) {
	for _, key := range sortedKeys(found) {
		count := strconv.Itoa(len(found[key]))

		if isPackageWide(key.item) {
			count = "*"
		}

		fmt.Printf("debt %-32s %-34s %s\n", key.pkg, key.item, count)
	}
}

func report(found map[debtKey][]string, cfg config, files []sourceFile) int {
	exitCode := 0
	present := map[string]bool{}

	for _, file := range files {
		present[file.pkg] = true
	}

	for pkg := range cfg.roles {
		if !present[pkg] {
			fmt.Printf("STALE   role for %s: package not found; fix %s\n", pkg, configPath)
			exitCode = 1
		}
	}

	violations := 0

	for _, key := range sortedKeys(found) {
		places := found[key]
		allowance, frozen := cfg.debts[key]

		switch {
		case frozen && allowance.wildcard:
			continue
		case frozen && len(places) <= allowance.count:
			if len(places) < allowance.count {
				fmt.Printf("SHRINK  %s %s: %d left, frozen at %d; lower line %d to %d\n",
					key.pkg, key.item, len(places), allowance.count, allowance.line, len(places))
			}

			continue
		}

		violations++
		exitCode = 1

		if frozen {
			fmt.Printf("FAIL    %s %s: %d, frozen at %d\n", key.pkg, key.item, len(places), allowance.count)
		} else {
			fmt.Printf("FAIL    %s %s: %d, none allowed\n", key.pkg, key.item, len(places))
		}

		for _, place := range places {
			fmt.Printf("          %s\n", place)
		}
	}

	for _, key := range sortedKeys(cfg.debts) {
		if _, still := found[key]; !still {
			fmt.Printf("CLEARED %s %s: no longer present; delete line %d\n", key.pkg, key.item, cfg.debts[key].line)
		}
	}

	switch {
	case exitCode == 0:
		fmt.Printf("ok: %d files, %d frozen debts, no new boundary violations\n", len(files), len(cfg.debts))
	case violations > 0:
		fmt.Printf("%d boundary violation(s). Fix the code; never raise or add a debt line to pass.\n", violations)
	default:
		fmt.Printf("%s names packages that no longer exist.\n", configPath)
	}

	return exitCode
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "check_boundaries: "+format+"\n", args...)
	os.Exit(2)
}
