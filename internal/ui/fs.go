package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// GetFileSystem returns the HTTP file system for the frontend assets.
// The frontend is built by running `npm run build` in the cyan-ui directory,
// which outputs to internal/ui/dist/.
func GetFileSystem() http.FileSystem {
	// Sub-directory is needed because vite outputs to dist/
	fsys, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return http.FS(fsys)
}

// GetFS returns the raw filesystem interface for the frontend assets.
// Useful for custom file serving logic.
func GetFS() fs.FS {
	fsys, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return fsys
}
