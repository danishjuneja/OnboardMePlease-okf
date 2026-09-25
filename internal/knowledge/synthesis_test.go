package knowledge

import (
	"onboardmeplease/internal/grounding"
	"strings"
	"testing"
)

func TestPartitionAccountsForScatteredSourceWithoutPrefixSampling(t *testing.T) {
	sources := []grounding.Source{}
	for i := 0; i < 1100; i++ {
		sources = append(sources, grounding.Source{ID: stableID(string(rune(i))), Path: "scattered/code", Content: strings.Repeat("x", 101)})
	}
	sources = append(sources, grounding.Source{ID: "oversize", Content: strings.Repeat("x", UnitBytes+1)})
	units, oversize := Partition(sources)
	seen := map[string]bool{}
	for _, unit := range units {
		size := 0
		for _, source := range unit {
			if seen[source.ID] {
				t.Fatal("duplicate source")
			}
			seen[source.ID] = true
			size += len(source.Content)
		}
		if size > UnitBytes || len(unit) > 12 {
			t.Fatal("work unit exceeded budget")
		}
	}
	if len(seen) != 1100 || oversize != 1 {
		t.Fatalf("coverage lost: %d, %d", len(seen), oversize)
	}
}
