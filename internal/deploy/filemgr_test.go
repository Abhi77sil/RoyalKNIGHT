package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileMgr_DirectoryListingAndSafety(t *testing.T) {
	root := t.TempDir()

	_ = os.WriteFile(filepath.Join(root, "index.html"), []byte("<h1>test</h1>"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "css"), 0755)
	_ = os.WriteFile(filepath.Join(root, "css", "main.css"), []byte("body{}"), 0644)

	// List root
	entries, err := ListDirectory(root, "")
	if err != nil {
		t.Fatalf("ListDirectory failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Test Path Traversal Protection
	_, err = ResolveSafePath(root, "../../../etc/shadow")
	if err == nil {
		t.Fatal("expected path traversal detection, got nil")
	}

	// Read & Write
	content, err := ReadFileContent(root, "index.html")
	if err != nil || content != "<h1>test</h1>" {
		t.Fatalf("unexpected content: %s (err: %v)", content, err)
	}

	if err := WriteFileContent(root, "index.html", "<h1>updated</h1>"); err != nil {
		t.Fatalf("WriteFileContent failed: %v", err)
	}
	updated, _ := ReadFileContent(root, "index.html")
	if updated != "<h1>updated</h1>" {
		t.Fatalf("expected updated content, got %s", updated)
	}

	// Create and Delete
	if err := CreateEntry(root, "script.js", false); err != nil {
		t.Fatalf("CreateEntry failed: %v", err)
	}
	if err := DeleteEntry(root, "script.js"); err != nil {
		t.Fatalf("DeleteEntry failed: %v", err)
	}
}
