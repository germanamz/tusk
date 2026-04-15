package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testMaxSize int64 = 1 << 20 // 1 MB

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture %s: %v", path, err)
	}
}

// stdinPipe returns an *os.File reader preloaded with content. The write end
// is closed before returning so io.ReadAll on the reader terminates cleanly.
func stdinPipe(t *testing.T, content string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("writing to pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestExpandRefs_WholeValueFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "foo.txt"), "hello")

	got, err := expandRefs("@./foo.txt", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestExpandRefs_MidString(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "foo.txt"), "hello")

	got, err := expandRefs("prefix @./foo.txt suffix", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "prefix hello suffix" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_MultipleRefs(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "one.txt"), "ONE")
	writeFile(t, filepath.Join(dir, "two.txt"), "TWO")

	got, err := expandRefs("a @./one.txt b @./two.txt", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "a ONE b TWO" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_QuotedPathWithSpace(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "my file.txt"), "spaced")

	got, err := expandRefs(`@"./my file.txt"`, nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "spaced" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_EscapeAtAt(t *testing.T) {
	got, err := expandRefs("@@mention please", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "@mention please" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_EscapeAtAtMidString(t *testing.T) {
	got, err := expandRefs("hi @@literal bye", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hi @literal bye" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_WordInternalAt(t *testing.T) {
	got, err := expandRefs("email@example.com", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "email@example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_BareAtWithSpace(t *testing.T) {
	_, err := expandRefs("foo @ bar", nil, testMaxSize)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bare @ is not a valid reference") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandRefs_BareAtEndOfString(t *testing.T) {
	_, err := expandRefs("trailing @", nil, testMaxSize)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bare @ is not a valid reference") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandRefs_EmptyQuotedPath(t *testing.T) {
	_, err := expandRefs(`@""`, nil, testMaxSize)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty path after @") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandRefs_UnclosedQuotedPath(t *testing.T) {
	_, err := expandRefs(`@"./baz`, nil, testMaxSize)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unclosed quoted path after @") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandRefs_MissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	_, err := expandRefs("@./nope.txt", nil, testMaxSize)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no such file") || !strings.Contains(err.Error(), "./nope.txt") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandRefs_BinaryFileEarlyNUL(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	data := []byte("hello\x00world")
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := expandRefs("@./bin.dat", nil, testMaxSize)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "binary file") || !strings.Contains(err.Error(), "./bin.dat") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandRefs_BinaryFileLateNUL(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// NUL at offset 10000 — past the 8 KB scan window.
	data := make([]byte, 10001)
	for i := range data {
		data[i] = 'a'
	}
	data[10000] = 0
	if err := os.WriteFile(filepath.Join(dir, "late.dat"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := expandRefs("@./late.dat", nil, testMaxSize)
	if err != nil {
		t.Fatalf("expected silent passthrough, got error: %v", err)
	}
	if len(got) != 10001 {
		t.Fatalf("unexpected length: %d", len(got))
	}
}

func TestExpandRefs_OverSizeCap(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	big := strings.Repeat("a", 200)
	writeFile(t, filepath.Join(dir, "big.txt"), big)

	_, err := expandRefs("@./big.txt", nil, 100)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exceeds") || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandRefs_StdinHappyPath(t *testing.T) {
	r := stdinPipe(t, "from stdin")

	got, err := expandRefs("@-", r, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from stdin" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_StdinMidString(t *testing.T) {
	r := stdinPipe(t, "X")

	got, err := expandRefs("prefix @- suffix", r, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "prefix X suffix" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_StdinTwice(t *testing.T) {
	r := stdinPipe(t, "whatever")

	_, err := expandRefs("@- @-", r, testMaxSize)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stdin referenced more than once") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandRefs_StdinNil(t *testing.T) {
	_, err := expandRefs("@-", nil, testMaxSize)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stdin is a terminal, not a pipe") {
		t.Fatalf("got %v", err)
	}
}

func TestExpandRefs_SubstitutedContentNotRescanned(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "first.txt"), "@./other.txt")
	writeFile(t, filepath.Join(dir, "other.txt"), "nested")

	got, err := expandRefs("@./first.txt", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "@./other.txt" {
		t.Fatalf("got %q — substituted content was re-scanned", got)
	}
}

func TestExpandRefs_RelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "x.txt"), "rel")

	got, err := expandRefs("@./x.txt", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "rel" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_HomeRelativePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, "tusk-test.txt"), "home")

	got, err := expandRefs("@~/tusk-test.txt", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "home" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_EmptyInput(t *testing.T) {
	got, err := expandRefs("", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandRefs_PlainText(t *testing.T) {
	got, err := expandRefs("plain text no refs", nil, testMaxSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain text no refs" {
		t.Fatalf("got %q", got)
	}
}

// TestExpandRefsWithState_StdinOnceAcrossCalls locks in the cross-call
// stdin-once invariant used by runCreate/runModify when both `title=@-` and
// `description=@-` appear in one invocation. Neither individual raw string
// contains two `@-` tokens, but the shared state must still error on the
// second call.
func TestExpandRefsWithState_StdinOnceAcrossCalls(t *testing.T) {
	r := stdinPipe(t, "X")
	state := &expandState{}

	first, err := expandRefsWithState("@-", r, testMaxSize, state)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if first != "X" {
		t.Fatalf("first call: got %q, want %q", first, "X")
	}

	_, err = expandRefsWithState("@-", r, testMaxSize, state)
	if err == nil {
		t.Fatal("second call: expected stdin-once error")
	}
	if !strings.Contains(err.Error(), "stdin referenced more than once in one invocation") {
		t.Fatalf("second call: got %v", err)
	}
}
