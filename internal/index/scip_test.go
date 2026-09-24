package index

import (
	"testing"

	scip "github.com/scip-code/scip/bindings/go/scip"
)

func TestSCIPRangeAndPathValidation(t *testing.T) {
	start, end, ok := scipLineRange(&scip.Occurrence{Range: []int32{3, 2, 4, 0}})
	if !ok || start != 4 || end != 4 {
		t.Fatalf("range = %d-%d, ok=%v", start, end, ok)
	}
	for _, path := range []string{"/secret", "../escape.go", "a/../escape.go", "a\\b.go", "a//b.go"} {
		if validSCIPPath(path) {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
	if !validSCIPPath("src/main.go") {
		t.Fatal("ordinary source path rejected")
	}
	if scipKey("one.go", "local 1") == scipKey("two.go", "local 1") {
		t.Fatal("local SCIP symbols crossed files")
	}
}
