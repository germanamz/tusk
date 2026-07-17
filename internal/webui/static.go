package webui

import (
	"io/fs"
	"net/http"
)

// StaticHandler serves the embedded frontend rooted at subdir within fsys.
func StaticHandler(fsys fs.FS, subdir string) http.Handler {
	sub, err := fs.Sub(fsys, subdir)

	if err != nil {
		return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			http.Error(writer, "static assets unavailable", http.StatusInternalServerError)
		})
	}

	return http.FileServerFS(sub)
}
