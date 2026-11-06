package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"
)

func discoverDrives() ([]string, error) {
	if runtime.GOOS != "windows" {
		root, err := filepath.Abs(string(filepath.Separator))
		if err != nil {
			return nil, err
		}
		return []string{root}, nil
	}

	var roots []string
	for letter := 'A'; letter <= 'Z'; letter++ {
		root := string(letter) + ":" + string(filepath.Separator)
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			roots = append(roots, root)
		}
	}
	return roots, nil
}

func driveNames(roots []string) []string {
	names := make([]string, 0, len(roots))
	for _, root := range roots {
		names = append(names, driveName(root))
	}
	return names
}

func driveName(root string) string {
	if volume := filepath.VolumeName(root); volume != "" {
		return volume
	}
	return root
}

func (e *explorer) resolve(requested string) (string, error) {
	fullPath, err := filepath.Abs(filepath.Clean(requested))
	if err != nil {
		return "", err
	}
	if !withinDrives(fullPath, e.driveRoots) {
		return "", fs.ErrPermission
	}
	// Resolve existing links so junctions and symlinks cannot escape a listed drive.
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err == nil {
		if !withinDrives(resolved, e.driveRoots) {
			return "", fs.ErrPermission
		}
		fullPath = resolved
	}
	return fullPath, nil
}

func withinDrives(path string, roots []string) bool {
	for _, root := range roots {
		root, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func isDriveRoot(path string, roots []string) bool {
	for _, root := range roots {
		if samePath(path, root) {
			return true
		}
	}
	return false
}

func fileAttributes(info os.FileInfo, name string) (string, bool, bool, bool) {
	var flags []string
	if info.IsDir() {
		flags = append(flags, "Directory")
	}
	var raw uint64
	attrs := info.Sys()
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
	if hidden {
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
	if len(flags) == 0 {
		return "Normal", hidden, systemAttr, readOnly
	}
	return strings.Join(flags, ", "), hidden, systemAttr, readOnly
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

func samePath(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
