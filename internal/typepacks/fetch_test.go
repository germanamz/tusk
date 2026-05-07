package typepacks_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/typepacks"
)

func TestFetch_HTTPSuccess(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte(`[node-types.task]
properties = [{ name = "summary", type = "string" }]
`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, fetchErr := typepacks.Fetch(ctx, server.URL)

	if fetchErr != nil {
		test.Fatalf("Fetch: %v", fetchErr)
	}

	if !strings.Contains(string(body), "node-types.task") {
		test.Errorf("body = %q", body)
	}
}

func TestFetch_HTTP404(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, fetchErr := typepacks.Fetch(context.Background(), server.URL)

	if fetchErr == nil {
		test.Fatal("expected fetch error on 404")
	}

	if !strings.Contains(fetchErr.Error(), "404") {
		test.Errorf("err = %v", fetchErr)
	}
}

func TestFetch_FileURL(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "pack.toml")

	if writeErr := os.WriteFile(path, []byte("[node-types.task]\n"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	body, fetchErr := typepacks.Fetch(context.Background(), "file://"+path)

	if fetchErr != nil {
		test.Fatalf("Fetch file://: %v", fetchErr)
	}

	if !strings.Contains(string(body), "node-types.task") {
		test.Errorf("body = %q", body)
	}
}

func TestFetch_FileURLNotFound(test *testing.T) {
	_, fetchErr := typepacks.Fetch(context.Background(), "file:///does/not/exist.toml")

	if fetchErr == nil {
		test.Fatal("expected error for missing file")
	}
}

func TestFetch_OversizeRejected(test *testing.T) {
	big := strings.Repeat("x", 2*1024*1024) // 2 MiB

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte(big))
	}))
	defer server.Close()

	_, fetchErr := typepacks.Fetch(context.Background(), server.URL)

	if fetchErr == nil {
		test.Fatal("expected oversize error")
	}

	if !strings.Contains(fetchErr.Error(), "size") {
		test.Errorf("err = %v", fetchErr)
	}
}

func TestFetch_TooManyRedirects(test *testing.T) {
	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, server.URL, http.StatusFound)
	}))
	defer server.Close()

	_, fetchErr := typepacks.Fetch(context.Background(), server.URL)

	if fetchErr == nil {
		test.Fatal("expected redirect-loop error")
	}
}
