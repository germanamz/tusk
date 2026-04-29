package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testMaxSize int64 = 1 << 20 // 1 MB

func writeFile(test *testing.T, path, content string) {
	test.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		test.Fatalf("writing fixture %s: %v", path, err)
	}
}

// stdinPipe returns an *os.File reader preloaded with content. The write end
// is closed before returning so io.ReadAll on the reader terminates cleanly.
func stdinPipe(test *testing.T, content string) *os.File {
	test.Helper()

	reader, writer, err := os.Pipe()

	if err != nil {
		test.Fatalf("os.Pipe: %v", err)
	}

	if _, err := writer.Write([]byte(content)); err != nil {
		test.Fatalf("writing to pipe: %v", err)
	}

	if err := writer.Close(); err != nil {
		test.Fatalf("closing pipe writer: %v", err)
	}

	test.Cleanup(func() { _ = reader.Close() })
	return reader
}

func TestExpandRefs_WholeValueFile(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)
	writeFile(test, filepath.Join(dir, "foo.txt"), "hello")

	got, err := expandRefs("@./foo.txt", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "hello" {
		test.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestExpandRefs_MidString(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)
	writeFile(test, filepath.Join(dir, "foo.txt"), "hello")

	got, err := expandRefs("prefix @./foo.txt suffix", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "prefix hello suffix" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_MultipleRefs(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)
	writeFile(test, filepath.Join(dir, "one.txt"), "ONE")
	writeFile(test, filepath.Join(dir, "two.txt"), "TWO")

	got, err := expandRefs("a @./one.txt b @./two.txt", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "a ONE b TWO" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_QuotedPathWithSpace(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)
	writeFile(test, filepath.Join(dir, "my file.txt"), "spaced")

	got, err := expandRefs(`@"./my file.txt"`, nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "spaced" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_EscapeAtAt(test *testing.T) {
	got, err := expandRefs("@@mention please", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "@mention please" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_EscapeAtAtMidString(test *testing.T) {
	got, err := expandRefs("hi @@literal bye", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "hi @literal bye" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_WordInternalAt(test *testing.T) {
	got, err := expandRefs("email@example.com", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "email@example.com" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_BareAtWithSpace(test *testing.T) {
	_, err := expandRefs("foo @ bar", nil, testMaxSize)
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bare @ is not a valid reference") {
		test.Fatalf("got %v", err)
	}
}

func TestExpandRefs_BareAtEndOfString(test *testing.T) {
	_, err := expandRefs("trailing @", nil, testMaxSize)
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bare @ is not a valid reference") {
		test.Fatalf("got %v", err)
	}
}

func TestExpandRefs_EmptyQuotedPath(test *testing.T) {
	_, err := expandRefs(`@""`, nil, testMaxSize)
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty path after @") {
		test.Fatalf("got %v", err)
	}
}

func TestExpandRefs_UnclosedQuotedPath(test *testing.T) {
	_, err := expandRefs(`@"./baz`, nil, testMaxSize)
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unclosed quoted path after @") {
		test.Fatalf("got %v", err)
	}
}

func TestExpandRefs_MissingFile(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)

	_, err := expandRefs("@./nope.txt", nil, testMaxSize)
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no such file") || !strings.Contains(err.Error(), "./nope.txt") {
		test.Fatalf("got %v", err)
	}
}

func TestExpandRefs_BinaryFileEarlyNUL(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)
	data := []byte("hello\x00world")
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), data, 0o644); err != nil {
		test.Fatal(err)
	}

	_, err := expandRefs("@./bin.dat", nil, testMaxSize)
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "binary file") || !strings.Contains(err.Error(), "./bin.dat") {
		test.Fatalf("got %v", err)
	}
}

func TestExpandRefs_BinaryFileLateNUL(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)
	// NUL at offset 10000 — past the 8 KB scan window.
	data := make([]byte, 10001)
	for index := range data {
		data[index] = 'a'
	}
	data[10000] = 0
	if err := os.WriteFile(filepath.Join(dir, "late.dat"), data, 0o644); err != nil {
		test.Fatal(err)
	}

	got, err := expandRefs("@./late.dat", nil, testMaxSize)

	if err != nil {
		test.Fatalf("expected silent passthrough, got error: %v", err)
	}

	if len(got) != 10001 {
		test.Fatalf("unexpected length: %d", len(got))
	}
}

func TestExpandRefs_OverSizeCap(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)
	big := strings.Repeat("a", 200)
	writeFile(test, filepath.Join(dir, "big.txt"), big)

	_, err := expandRefs("@./big.txt", nil, 100)
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exceeds") || !strings.Contains(err.Error(), "limit") {
		test.Fatalf("got %v", err)
	}
}

func TestExpandRefs_StdinHappyPath(test *testing.T) {
	reader := stdinPipe(test, "from stdin")

	got, err := expandRefs("@-", reader, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "from stdin" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_StdinMidString(test *testing.T) {
	reader := stdinPipe(test, "X")

	got, err := expandRefs("prefix @- suffix", reader, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "prefix X suffix" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_StdinTwice(test *testing.T) {
	reader := stdinPipe(test, "whatever")

	_, err := expandRefs("@- @-", reader, testMaxSize)
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stdin referenced more than once") {
		test.Fatalf("got %v", err)
	}
}

func TestExpandRefs_StdinNil(test *testing.T) {
	_, err := expandRefs("@-", nil, testMaxSize)
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stdin is a terminal, not a pipe") {
		test.Fatalf("got %v", err)
	}
}

func TestExpandRefs_SubstitutedContentNotRescanned(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)
	writeFile(test, filepath.Join(dir, "first.txt"), "@./other.txt")
	writeFile(test, filepath.Join(dir, "other.txt"), "nested")

	got, err := expandRefs("@./first.txt", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "@./other.txt" {
		test.Fatalf("got %q — substituted content was re-scanned", got)
	}
}

func TestExpandRefs_RelativePath(test *testing.T) {
	dir := test.TempDir()
	test.Chdir(dir)
	writeFile(test, filepath.Join(dir, "x.txt"), "rel")

	got, err := expandRefs("@./x.txt", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "rel" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_HomeRelativePath(test *testing.T) {
	home := test.TempDir()
	test.Setenv("HOME", home)
	writeFile(test, filepath.Join(home, "tusk-test.txt"), "home")

	got, err := expandRefs("@~/tusk-test.txt", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "home" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_EmptyInput(test *testing.T) {
	got, err := expandRefs("", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "" {
		test.Fatalf("got %q", got)
	}
}

func TestExpandRefs_PlainText(test *testing.T) {
	got, err := expandRefs("plain text no refs", nil, testMaxSize)

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	if got != "plain text no refs" {
		test.Fatalf("got %q", got)
	}
}

// TestExpandRefsWithState_StdinOnceAcrossCalls locks in the cross-call
// stdin-once invariant used by runCreate/runModify when both `title=@-` and
// `description=@-` appear in one invocation. Neither individual raw string
// contains two `@-` tokens, but the shared state must still error on the
// second call.
func TestExpandRefsWithState_StdinOnceAcrossCalls(test *testing.T) {
	reader := stdinPipe(test, "X")
	state := &expandState{}

	first, err := expandRefsWithState("@-", reader, testMaxSize, state)

	if err != nil {
		test.Fatalf("first call: unexpected error: %v", err)
	}

	if first != "X" {
		test.Fatalf("first call: got %q, want %q", first, "X")
	}

	_, err = expandRefsWithState("@-", reader, testMaxSize, state)
	if err == nil {
		test.Fatal("second call: expected stdin-once error")
	}
	if !strings.Contains(err.Error(), "stdin referenced more than once in one invocation") {
		test.Fatalf("second call: got %v", err)
	}
}
