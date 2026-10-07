package deploy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	IsDir     bool      `json:"is_dir"`
	ModTime   time.Time `json:"mod_time"`
	Extension string    `json:"extension"`
}

const MaxEditSizeBytes = 5 * 1024 * 1024 // 5 MB max editable file

// ResolveSafePath validates that target subPath stays strictly inside rootDir.
func ResolveSafePath(rootDir, subPath string) (string, error) {
	cleanRoot := filepath.Clean(rootDir)
	target := filepath.Clean(filepath.Join(cleanRoot, filepath.FromSlash(subPath)))

	cleanPrefix := cleanRoot + string(os.PathSeparator)
	if target != cleanRoot && !strings.HasPrefix(target, cleanPrefix) {
		return "", fmt.Errorf("access denied: path traversal detected")
	}
	return target, nil
}

// ListDirectory lists contents of a directory inside rootDir.
func ListDirectory(rootDir, subPath string) ([]FileEntry, error) {
	target, err := ResolveSafePath(rootDir, subPath)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	relPrefix, err := filepath.Rel(rootDir, target)
	if err != nil || relPrefix == "." {
		relPrefix = ""
	}

	result := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}

		relPath := filepath.Join(relPrefix, e.Name())
		ext := strings.ToLower(filepath.Ext(e.Name()))

		result = append(result, FileEntry{
			Name:      e.Name(),
			Path:      filepath.ToSlash(relPath),
			Size:      info.Size(),
			IsDir:     e.IsDir(),
			ModTime:   info.ModTime().UTC(),
			Extension: ext,
		})
	}
	return result, nil
}

// ReadFileContent reads text content of a file within rootDir.
func ReadFileContent(rootDir, subPath string) (string, error) {
	target, err := ResolveSafePath(rootDir, subPath)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("cannot open directory as text file")
	}
	if info.Size() > MaxEditSizeBytes {
		return "", fmt.Errorf("file size (%d bytes) exceeds 5MB editor limit", info.Size())
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(data), nil
}

// WriteFileContent writes text content safely into a file.
func WriteFileContent(rootDir, subPath, content string) error {
	target, err := ResolveSafePath(rootDir, subPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create parent directories: %w", err)
	}

	return os.WriteFile(target, []byte(content), 0644)
}

// CreateEntry creates a new file or directory inside rootDir.
func CreateEntry(rootDir, subPath string, isDir bool) error {
	target, err := ResolveSafePath(rootDir, subPath)
	if err != nil {
		return err
	}

	if isDir {
		return os.MkdirAll(target, 0755)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	return f.Close()
}

// DeleteEntry deletes a file or directory inside rootDir.
func DeleteEntry(rootDir, subPath string) error {
	if subPath == "" || subPath == "/" || subPath == "." {
		return fmt.Errorf("cannot delete root directory")
	}

	target, err := ResolveSafePath(rootDir, subPath)
	if err != nil {
		return err
	}
	if target == filepath.Clean(rootDir) {
		return fmt.Errorf("cannot delete root directory")
	}

	return os.RemoveAll(target)
}

// RenameEntry renames or moves a file or directory within rootDir.
func RenameEntry(rootDir, oldSubPath, newSubPath string) error {
	oldTarget, err := ResolveSafePath(rootDir, oldSubPath)
	if err != nil {
		return err
	}
	newTarget, err := ResolveSafePath(rootDir, newSubPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(newTarget), 0755); err != nil {
		return fmt.Errorf("failed to create destination directories: %w", err)
	}

	return os.Rename(oldTarget, newTarget)
}


// CopyDir recursively copies a directory tree.
func CopyDir(srcDir, dstDir string) error {
	srcClean := filepath.Clean(srcDir)
	dstClean := filepath.Clean(dstDir)

	if err := os.MkdirAll(dstClean, 0755); err != nil {
		return err
	}

	return filepath.Walk(srcClean, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcClean, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		target := filepath.Join(dstClean, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
