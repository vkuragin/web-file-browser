package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed index.html
var appFiles embed.FS

const driveListPath = "drives://"

type explorer struct {
	driveRoots []string
}

type entry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Size       *int64 `json:"size"`
	Modified   string `json:"modified"`
	Attributes string `json:"attributes"`
	Hidden     bool   `json:"hidden"`
	System     bool   `json:"system"`
}

type listing struct {
	Path   string  `json:"path"`
	Parent *string `json:"parent"`
	Items  []entry `json:"items"`
}

type properties struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Kind       string `json:"kind"`
	Size       *int64 `json:"size"`
	Modified   string `json:"modified"`
	Created    string `json:"created"`
	Accessed   string `json:"accessed"`
	Attributes string `json:"attributes"`
	Hidden     bool   `json:"hidden"`
	System     bool   `json:"system"`
	ReadOnly   bool   `json:"readonly"`
}

func newExplorer(driveRoots []string) *explorer {
	return &explorer{driveRoots: driveRoots}
}

func (e *explorer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/list", e.handleList)
	mux.HandleFunc("GET /api/properties", e.handleProperties)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		data, err := appFiles.ReadFile("index.html")
		if err != nil {
			http.Error(w, "app page unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
	return mux
}

func (e *explorer) handleList(w http.ResponseWriter, r *http.Request) {
	requested := r.URL.Query().Get("path")
	if requested == "" || requested == driveListPath {
		writeJSON(w, e.driveListing())
		return
	}
	fullPath, err := e.resolve(requested)
	if err != nil {
		writeError(w, err)
		return
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		writeError(w, err)
		return
	}
	if !info.IsDir() {
		http.Error(w, "that item is not a folder", http.StatusBadRequest)
		return
	}
	children, err := os.ReadDir(fullPath)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]entry, 0, len(children))
	for _, child := range children {
		childPath := filepath.Join(fullPath, child.Name())
		childInfo, err := child.Info()
		if err != nil {
			continue
		}
		attrs, hidden, system, _ := fileAttributes(childInfo, child.Name())
		kind := "file"
		var size *int64
		if childInfo.IsDir() {
			kind = "folder"
		} else {
			length := childInfo.Size()
			size = &length
		}
		items = append(items, entry{Name: child.Name(), Path: childPath, Kind: kind, Size: size, Modified: childInfo.ModTime().UTC().Format(time.RFC3339Nano), Attributes: attrs, Hidden: hidden, System: system})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind == "folder"
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	var parent *string
	if isDriveRoot(fullPath, e.driveRoots) {
		parent = stringPointer(driveListPath)
	} else {
		p := filepath.Dir(fullPath)
		parent = &p
	}
	writeJSON(w, listing{Path: fullPath, Parent: parent, Items: items})
}

func (e *explorer) driveListing() listing {
	items := make([]entry, 0, len(e.driveRoots))
	for _, root := range e.driveRoots {
		items = append(items, entry{Name: driveName(root), Path: root, Kind: "folder", Attributes: "Drive"})
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	return listing{Path: driveListPath, Items: items}
}

func (e *explorer) handleProperties(w http.ResponseWriter, r *http.Request) {
	requested := r.URL.Query().Get("path")
	if requested == "" || requested == driveListPath {
		http.Error(w, "select a drive or item to view properties", http.StatusBadRequest)
		return
	}
	fullPath, err := e.resolve(requested)
	if err != nil {
		writeError(w, err)
		return
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		writeError(w, err)
		return
	}
	attrs, hidden, system, readOnly := fileAttributes(info, filepath.Base(fullPath))
	name := info.Name()
	kind, typ := "File", "File"
	var size *int64
	if info.IsDir() {
		kind, typ = "Folder", "Folder"
		if isDriveRoot(fullPath, e.driveRoots) {
			name, typ, attrs = driveName(fullPath), "Drive", "Drive"
		}
	} else {
		length := info.Size()
		size = &length
		if ext := strings.TrimPrefix(filepath.Ext(name), "."); ext != "" {
			typ = strings.ToUpper(ext) + " file"
		}
	}
	created, accessed := windowsFileTime(info, "CreationTime"), windowsFileTime(info, "LastAccessTime")
	if created.IsZero() {
		created = info.ModTime()
	}
	if accessed.IsZero() {
		accessed = info.ModTime()
	}
	writeJSON(w, properties{Name: name, Path: fullPath, Type: typ, Kind: kind, Size: size, Modified: info.ModTime().UTC().Format(time.RFC3339Nano), Created: created.UTC().Format(time.RFC3339Nano), Accessed: accessed.UTC().Format(time.RFC3339Nano), Attributes: attrs, Hidden: hidden, System: system, ReadOnly: readOnly})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, fs.ErrPermission):
		status = http.StatusForbidden
	case errors.Is(err, fs.ErrNotExist):
		status = http.StatusNotFound
	}
	http.Error(w, err.Error(), status)
}

func stringPointer(value string) *string { return &value }
