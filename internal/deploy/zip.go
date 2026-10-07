package deploy

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxDecompressedBytes = 500 * 1024 * 1024 // 500 MB
	MaxFileCount         = 10000
)

// ExtractZip unpacks a zip archive into targetDir with strict Zip Slip and Zip Bomb protection.
func ExtractZip(zipPath, targetDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer reader.Close()

	cleanTargetDir := filepath.Clean(targetDir)
	if err := os.MkdirAll(cleanTargetDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination root directory: %w", err)
	}

	var totalExtractedBytes int64
	var totalFiles int

	for _, file := range reader.File {
		totalFiles++
		if totalFiles > MaxFileCount {
			return fmt.Errorf("archive exceeds maximum file count limit (%d)", MaxFileCount)
		}

		// Mitigate Zip Slip: validate target path remains strictly within cleanTargetDir
		cleanedName := filepath.Clean(file.Name)
		if strings.HasPrefix(cleanedName, "..") || filepath.IsAbs(cleanedName) {
			return fmt.Errorf("illegal relative or absolute path in archive: %s", file.Name)
		}

		destPath := filepath.Join(cleanTargetDir, cleanedName)
		destPrefix := cleanTargetDir + string(os.PathSeparator)
		if !strings.HasPrefix(destPath, destPrefix) && destPath != cleanTargetDir {
			return fmt.Errorf("zip slip vulnerability detected: target %s escapes destination directory", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
			continue
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory for %s: %w", destPath, err)
		}

		// Prohibit symlinks to prevent directory traversal
		if file.Mode()&os.ModeSymlink != 0 {
			continue
		}

		srcFile, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to read entry %s: %w", file.Name, err)
		}

		outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			srcFile.Close()
			return fmt.Errorf("failed to open destination file %s: %w", destPath, err)
		}

		// Copy with byte count tracking to prevent zip bomb decompression explosion
		written, err := io.Copy(outFile, io.LimitReader(srcFile, MaxDecompressedBytes-totalExtractedBytes+1))
		outFile.Close()
		srcFile.Close()

		if err != nil {
			return fmt.Errorf("failed to write file %s: %w", destPath, err)
		}

		totalExtractedBytes += written
		if totalExtractedBytes > MaxDecompressedBytes {
			return fmt.Errorf("archive exceeds maximum allowed uncompressed payload size limit")
		}
	}

	// Unwrap single top-level directory if entire archive is wrapped inside one folder
	unwrapSingleDirectory(cleanTargetDir)

	// Ensure parent and target directories have execute/read permissions (0755)
	_ = os.Chmod(filepath.Dir(cleanTargetDir), 0755)
	_ = os.Chmod(cleanTargetDir, 0755)

	// Enforce world-readable permissions for web servers (Nginx/Caddy)
	_ = filepath.Walk(cleanTargetDir, func(path string, info os.FileInfo, err error) error {
		if err == nil {
			if info.IsDir() {
				_ = os.Chmod(path, 0755)
			} else {
				_ = os.Chmod(path, 0644)
			}
		}
		return nil
	})

	return nil
}

func unwrapSingleDirectory(targetDir string) {
	entries, err := os.ReadDir(targetDir)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return
	}

	singleDir := filepath.Join(targetDir, entries[0].Name())
	subEntries, err := os.ReadDir(singleDir)
	if err != nil {
		return
	}

	for _, sub := range subEntries {
		src := filepath.Join(singleDir, sub.Name())
		dest := filepath.Join(targetDir, sub.Name())
		_ = os.Rename(src, dest)
	}
	_ = os.Remove(singleDir)
}
