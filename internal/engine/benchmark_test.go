package engine

import (
	"os"
	"testing"
)

// Set OMP_BENCH_REPO to an already-indexed disposable repository.
func BenchmarkRepository(b *testing.B) {
	root := os.Getenv("OMP_BENCH_REPO")
	if root == "" {
		b.Skip("set OMP_BENCH_REPO")
	}
	previous, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}
	defer os.Chdir(previous)
	if err := os.Chdir(root); err != nil {
		b.Fatal(err)
	}
	w, err := Discover()
	if err != nil {
		b.Fatal(err)
	}
	s, err := Open(w, false)
	if err != nil {
		b.Fatal(err)
	}
	defer s.DB.Close()
	b.Run("audit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if current, err := s.Audit(); err != nil || !current {
				b.Fatalf("audit: %v, current=%v", err, current)
			}
		}
	})
	b.Run("audit_metadata", func(b *testing.B) {
		s.Fast = true
		defer func() { s.Fast = false }()
		for i := 0; i < b.N; i++ {
			if current, err := s.Audit(); err != nil || !current {
				b.Fatalf("audit: %v, current=%v", err, current)
			}
		}
	})
	b.Run("investigate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.Investigate("How does the broker handle a producer request from receipt through writing records?", 3); err != nil {
				b.Fatal(err)
			}
		}
	})
}
