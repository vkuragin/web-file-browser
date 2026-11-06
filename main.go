package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"
)

//go:embed index.html
var appFiles embed.FS

type explorer struct {
	root string
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

func main() {
	root, err := systemRoot()
	if err != nil {
		log.Fatal(err)
	}
	port := os.Getenv("FILE_EXPLORER_PORT")
	if port == "" {
		port = "8765"
	}

	server := &http.Server{
		Addr:              "127.0.0.1:" + port,
		Handler:           (&explorer{root: root}).routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	log.Printf("File Explorer running at http://localhost:%s/ (root: %s)", port, root)
	log.Printf("Press Ctrl+C to stop.")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	log.Printf("File Explorer stopped; port %s released.", port)
}

func systemRoot() (string, error) {
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		volume := filepath.VolumeName(systemRoot)
		if volume != "" {
			return volume + string(filepath.Separator), nil
		}
	}
	if drive := os.Getenv("SystemDrive"); drive != "" {
		return drive + string(filepath.Separator), nil
	}
	return filepath.Abs(string(filepath.Separator))
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
	if requested == "" {
		requested = e.root
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
	if !samePath(fullPath, e.root) {
		p := filepath.Dir(fullPath)
		parent = &p
	}
	writeJSON(w, listing{Path: fullPath, Parent: parent, Items: items})
}

func (e *explorer) handleProperties(w http.ResponseWriter, r *http.Request) {
	requested := r.URL.Query().Get("path")
	if requested == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
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
	if samePath(fullPath, e.root) {
		name = fullPath
	}
	kind, typ := "File", "File"
	var size *int64
	if info.IsDir() {
		kind, typ = "Folder", "Folder"
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

func windowsFileTime(info os.FileInfo, fieldName string) time.Time {
	sys := reflect.ValueOf(info.Sys())
	if sys.Kind() != reflect.Pointer || sys.IsNil() {
		return time.Time{}
	}
	field := sys.Elem().FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.Struct {
		return time.Time{}
	}
	low, high := field.FieldByName("LowDateTime"), field.FieldByName("HighDateTime")
	if !low.IsValid() || !high.IsValid() || !low.CanUint() || !high.CanUint() {
		return time.Time{}
	}
	ticks := (high.Uint() << 32) | low.Uint()
	const windowsToUnixSeconds = 11644473600
	return time.Unix(int64(ticks/10_000_000)-windowsToUnixSeconds, int64(ticks%10_000_000)*100)
}

func (e *explorer) resolve(requested string) (string, error) {
	fullPath, err := filepath.Abs(filepath.Clean(requested))
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(filepath.Clean(e.root))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fs.ErrPermission
	}
	// Resolve existing links so a junction or symlink cannot escape the drive root.
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err == nil {
		resolvedRel, relErr := filepath.Rel(root, resolved)
		if relErr != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) || filepath.IsAbs(resolvedRel) {
			return "", fs.ErrPermission
		}
		fullPath = resolved
	}
	return fullPath, nil
}

func fileAttributes(info os.FileInfo, name string) (string, bool, bool, bool) {
	var flags []string
	if info.IsDir() {
		flags = append(flags, "Directory")
	}
	attrs := info.Sys()
	var raw uint64
	if attrs != nil {
		v := reflect.ValueOf(attrs)
		if v.Kind() == reflect.Pointer && !v.IsNil() {
			v = v.Elem()
			if v.Kind() == reflect.Struct {
				field := v.FieldByName("FileAttributes")
				if field.IsValid() && field.CanUint() {
					raw = field.Uint()
				}
			}
		}
	}
	hiddenAttr, systemAttr, readOnly := raw&2 != 0, raw&4 != 0, raw&1 != 0
	hidden := hiddenAttr || strings.HasPrefix(name, ".")
	if hiddenAttr {
		flags = append(flags, "Hidden")
	}
	if systemAttr {
		flags = append(flags, "System")
	}
	if readOnly {
		flags = append(flags, "ReadOnly")
	}
	if !info.IsDir() {
		flags = append(flags, "Archive")
	}
	if hidden && !hiddenAttr {
		flags = append(flags, "Hidden")
	}
	if len(flags) == 0 {
		return "Normal", hidden, systemAttr, readOnly
	}
	return strings.Join(flags, ", "), hidden, systemAttr, readOnly
}

func samePath(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
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
