package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiscoverDrivesReturnsUsableRoot(t *testing.T) {
	roots, err := discoverDrives()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) == 0 {
		t.Fatal("expected at least one drive root")
	}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			t.Errorf("drive root %q is not an accessible directory: %v", root, err)
		}
	}
}

func TestDiscoverDrivesIncludesWindowsSystemDrive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows system-drive check")
	}
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Skip("SystemRoot is not set")
	}
	systemDrive := filepath.VolumeName(systemRoot) + string(filepath.Separator)
	roots, err := discoverDrives()
	if err != nil {
		t.Fatal(err)
	}
	if !isDriveRoot(systemDrive, roots) {
		t.Fatalf("discovered drives %v do not include system drive %q", roots, systemDrive)
	}
}

func TestResolveAllowsAnyListedDriveAndRejectsUnlistedPath(t *testing.T) {
	first, second, outside := t.TempDir(), t.TempDir(), t.TempDir()
	e := newExplorer([]string{first, second})
	resolved, err := e.resolve(filepath.Join(second, "file.txt"))
	if err != nil || !samePath(resolved, filepath.Join(second, "file.txt")) {
		t.Fatalf("resolve second listed root = %q, %v", resolved, err)
	}
	if _, err := e.resolve(outside); err == nil {
		t.Fatal("expected unlisted path to be rejected")
	}
}

func TestRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require elevated privileges on Windows")
	}
	base := t.TempDir()
	root, outside := filepath.Join(base, "root"), filepath.Join(base, "outside")
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
	if _, err := newExplorer([]string{root}).resolve(link); err == nil {
		t.Fatal("expected symlink to outside drive to be rejected")
	}
}
