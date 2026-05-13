package state

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesSchemaIfNotExists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var version int
	if err := s.DB().QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if version != 1 {
		t.Errorf("schema_version = %d, want 1", version)
	}
}

func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s1.Close()

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	var version int
	if err := s2.DB().QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("query: %v", err)
	}
	if version != 1 {
		t.Errorf("version = %d, want 1", version)
	}
}
