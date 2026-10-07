package metrics

import (
	"testing"
)

func TestMonitor_Collect(t *testing.T) {
	m := NewMonitor()
	stats := m.Collect("/")

	if stats.MemTotalBytes == 0 {
		t.Fatal("expected non-zero total memory")
	}
	if stats.DiskTotalBytes == 0 {
		t.Fatal("expected non-zero total disk")
	}
	if stats.Goroutines <= 0 {
		t.Fatal("expected positive goroutine count")
	}
}
