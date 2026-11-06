package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
	return newExplorer([]string{root}), root
}

func TestDriveListIsExplorerRoot(t *testing.T) {
	e, root := newFixture(t)
	secondRoot := t.TempDir()
	e = newExplorer([]string{root, secondRoot})
	for _, target := range []string{"/api/list", "/api/list?path=" + url.QueryEscape(driveListPath)} {
		recorder := httptest.NewRecorder()
		e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		var got listing
		if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusOK || got.Path != driveListPath || got.Parent != nil {
			t.Fatalf("target %q: status=%d path=%q parent=%v", target, recorder.Code, got.Path, got.Parent)
		}
		if len(got.Items) != 2 || got.Items[0].Kind != "folder" || got.Items[1].Kind != "folder" {
			t.Fatalf("unexpected drive list: %#v", got.Items)
		}
		listed := map[string]bool{got.Items[0].Path: true, got.Items[1].Path: true}
		if !listed[root] || !listed[secondRoot] {
			t.Fatalf("drive list omitted a configured drive: %#v", got.Items)
		}
	}
}

func TestDriveRootListsFilesAndHasDriveListParent(t *testing.T) {
	e, root := newFixture(t)
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/list?path="+url.QueryEscape(root), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var got listing
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Parent == nil || *got.Parent != driveListPath {
		t.Fatalf("drive root parent=%v, want %q", got.Parent, driveListPath)
	}
	if len(got.Items) != 3 {
		t.Fatalf("got %d items, want 3: %#v", len(got.Items), got.Items)
	}
	if got.Items[0].Name != "Folder" || got.Items[0].Kind != "folder" {
		t.Fatalf("first item=%#v, want Folder directory first", got.Items[0])
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

func TestDriveRootProperties(t *testing.T) {
	e, root := newFixture(t)
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/properties?path="+url.QueryEscape(root), nil))
	var got properties
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || got.Kind != "Folder" || got.Type != "Drive" || got.Size != nil {
		t.Fatalf("status=%d properties=%#v", recorder.Code, got)
	}
}

func TestPropertiesReturnFileMetadata(t *testing.T) {
	e, root := newFixture(t)
	path := filepath.Join(root, "notes.txt")
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/properties?path="+url.QueryEscape(path), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var got properties
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "notes.txt" || got.Kind != "File" || got.Type != "TXT file" || got.Size == nil || *got.Size != 5 {
		t.Fatalf("unexpected properties: %#v", got)
	}
}

func TestRejectsPathsOutsideAllDrives(t *testing.T) {
	e, root := newFixture(t)
	for _, target := range []string{filepath.Dir(root), filepath.Join(root, "..", "outside")} {
		recorder := httptest.NewRecorder()
		e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/list?path="+url.QueryEscape(target), nil))
		if recorder.Code != http.StatusForbidden {
			t.Errorf("path %q status=%d, want 403", target, recorder.Code)
		}
	}
}

func TestListRejectsFile(t *testing.T) {
	e, root := newFixture(t)
	path := filepath.Join(root, "notes.txt")
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/list?path="+url.QueryEscape(path), nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", recorder.Code)
	}
}

func TestPropertiesRejectMissingPath(t *testing.T) {
	e, _ := newFixture(t)
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/properties", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", recorder.Code)
	}
}

func TestPageServed(t *testing.T) {
	e, _ := newFixture(t)
	recorder := httptest.NewRecorder()
	e.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "File Explorer") {
		t.Fatalf("page response status=%d body did not contain app", recorder.Code)
	}
}
