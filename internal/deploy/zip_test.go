package deploy

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractZip_SafeExtraction(t *testing.T) {
	tempDir := t.TempDir()
	zipPath := filepath.Join(tempDir, "valid.zip")
	destDir := filepath.Join(tempDir, "dest")

	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	f1, err := zw.Create("index.html")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	_, _ = f1.Write([]byte("<h1>Hello RoyalKnight</h1>"))

	f2, err := zw.Create("assets/style.css")
	if err != nil {
		t.Fatalf("failed to create sub entry: %v", err)
	}
	_, _ = f2.Write([]byte("body { color: blue; }"))
	_ = zw.Close()

	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write zip file: %v", err)
	}

	if err := ExtractZip(zipPath, destDir); err != nil {
		t.Fatalf("ExtractZip failed on valid archive: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(destDir, "index.html"))
	if err != nil || !strings.Contains(string(content), "Hello RoyalKnight") {
		t.Fatalf("unexpected content: %s (err: %v)", string(content), err)
	}
}

func TestExtractZip_ZipSlipDetection(t *testing.T) {
	tempDir := t.TempDir()
	zipPath := filepath.Join(tempDir, "malicious.zip")
	destDir := filepath.Join(tempDir, "dest")

	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	f, err := zw.Create("../../../evil.txt")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	_, _ = f.Write([]byte("malicious content"))
	_ = zw.Close()

	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write zip file: %v", err)
	}

	err = ExtractZip(zipPath, destDir)
	if err == nil {
		t.Fatal("expected ExtractZip to reject Zip Slip entry, but succeeded")
	}

	if !strings.Contains(err.Error(), "zip slip") && !strings.Contains(err.Error(), "illegal") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
