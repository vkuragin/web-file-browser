package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newFixture(t *testing.T) (*explorer, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dotfile"), []byte("hidden-ish"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &explorer{root: root}, root
}

func TestListIncludesFilesFoldersAndDotfiles(t *testing.T) {
	e, root := newFixture(t)
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/list?path="+urlQuery(root), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var got listing
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Parent != nil {
		t.Fatalf("root listing parent = %v, want nil", *got.Parent)
	}
	if len(got.Items) != 3 {
		t.Fatalf("got %d items, want 3: %#v", len(got.Items), got.Items)
	}
	if got.Items[0].Name != "Folder" || got.Items[0].Kind != "folder" {
		t.Fatalf("first item = %#v, want Folder directory first", got.Items[0])
	}
	var foundDotfile bool
	for _, item := range got.Items {
		if item.Name == ".dotfile" {
			foundDotfile = true
			if !item.Hidden {
				t.Error("dot-prefixed item should be marked hidden")
			}
		}
	}
	if !foundDotfile {
		t.Error("listing omitted dot-prefixed file")
	}
}

func TestPropertiesReturnFileMetadata(t *testing.T) {
	e, root := newFixture(t)
	path := filepath.Join(root, "notes.txt")
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/properties?path="+urlQuery(path), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var got properties
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "notes.txt" || got.Kind != "File" || got.Type != "TXT file" {
		t.Fatalf("unexpected properties: %#v", got)
	}
	if got.Size == nil || *got.Size != 5 {
		t.Fatalf("size = %v, want 5", got.Size)
	}
}

func TestListRejectsOutsideRoot(t *testing.T) {
	e, root := newFixture(t)
	outside := filepath.Dir(root)
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/list?path="+urlQuery(outside), nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestListRejectsTraversal(t *testing.T) {
	e, root := newFixture(t)
	traversal := filepath.Join(root, "..", "outside")
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/list?path="+urlQuery(traversal), nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestListRejectsFile(t *testing.T) {
	e, root := newFixture(t)
	path := filepath.Join(root, "notes.txt")
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/list?path="+urlQuery(path), nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestListMissingPathDefaultsToRoot(t *testing.T) {
	e, root := newFixture(t)
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/list", nil))
	var got listing
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !samePath(got.Path, root) {
		t.Fatalf("status=%d path=%q, want root %q", recorder.Code, got.Path, root)
	}
}

func TestPropertiesRejectTraversalAndMissingPath(t *testing.T) {
	e, root := newFixture(t)
	for _, target := range []string{filepath.Join(root, "..", "outside"), ""} {
		recorder := httptest.NewRecorder()
		e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/properties?path="+urlQuery(target), nil))
		if recorder.Code != http.StatusForbidden && recorder.Code != http.StatusBadRequest {
			t.Errorf("path %q returned status %d, want 403 or 400", target, recorder.Code)
		}
	}
}

func TestRootPropertyAndPage(t *testing.T) {
	e, root := newFixture(t)
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/properties?path="+urlQuery(root), nil))
	var got properties
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "Folder" || got.Size != nil {
		t.Fatalf("unexpected root properties: %#v", got)
	}
	page := httptest.NewRecorder()
	e.routes().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "File Explorer") {
		t.Fatalf("page response status=%d body did not contain app", page.Code)
	}
}

func TestRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require elevated privileges on Windows")
	}
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	e := &explorer{root: root}
	if _, err := e.resolve(link); err == nil {
		t.Fatal("expected symlink to outside root to be rejected")
	}
}

func urlQuery(value string) string {
	return url.QueryEscape(value)
}
