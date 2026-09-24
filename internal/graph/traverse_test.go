package graph

import (
	"context"
	"fmt"
	"testing"
)

func TestCyclePreservesBackEdgeAndTerminates(t *testing.T) {
	adjacency := map[string][]Edge{
		"a": {{FromID: "a", ToID: "b"}},
		"b": {{FromID: "b", ToID: "a"}},
	}
	calls := 0
	result, err := Traverse(context.Background(), "a", Limits{Hops: 6, Nodes: 200, Outgoing: 20}, func(_ context.Context, id string, limit int) ([]Edge, error) {
		calls++
		return adjacency[id], nil
	})
	if err != nil || len(result.NodeIDs) != 2 || len(result.Edges) != 2 || result.Truncated || calls != 2 {
		t.Fatalf("cycle traversal = %+v, calls=%d, err=%v", result, calls, err)
	}
}

func TestHubAndDepthLimitsReportTruncation(t *testing.T) {
	hub := make([]Edge, 1000)
	for i := range hub {
		hub[i] = Edge{FromID: "root", ToID: fmt.Sprintf("node-%d", i)}
	}
	result, err := Traverse(context.Background(), "root", Limits{Hops: 6, Nodes: 200, Outgoing: 20}, func(_ context.Context, id string, limit int) ([]Edge, error) {
		if id != "root" {
			return nil, nil
		}
		if len(hub) > limit {
			return hub[:limit], nil
		}
		return hub, nil
	})
	if err != nil || !result.Truncated || len(result.NodeIDs) != 21 || len(result.Edges) != 20 {
		t.Fatalf("hub traversal = %+v, err=%v", result, err)
	}
	result, err = Traverse(context.Background(), "0", Limits{Hops: 6, Nodes: 200, Outgoing: 20}, func(_ context.Context, id string, limit int) ([]Edge, error) {
		var number int
		_, _ = fmt.Sscanf(id, "%d", &number)
		return []Edge{{FromID: id, ToID: fmt.Sprint(number + 1)}}, nil
	})
	if err != nil || !result.Truncated || len(result.NodeIDs) != 7 {
		t.Fatalf("depth traversal = %+v, err=%v", result, err)
	}
}
