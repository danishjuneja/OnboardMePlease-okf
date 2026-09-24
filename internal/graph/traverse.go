package graph

import (
	"context"
)

type Edge struct {
	ID         string `json:"relation_id"`
	FromID     string `json:"from_id"`
	ToID       string `json:"to_id"`
	Kind       string `json:"kind"`
	Resolution string `json:"resolution"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
}

type Limits struct{ Hops, Nodes, Outgoing int }

type Result struct {
	NodeIDs   []string
	Edges     []Edge
	Truncated bool
}

type Fetch func(context.Context, string, int) ([]Edge, error)

type entry struct {
	id    string
	depth int
}

// Traverse records back-edges but expands each node at most once. The fetch
// limit asks for one extra edge so fan-out truncation is observable.
func Traverse(ctx context.Context, root string, limits Limits, fetch Fetch) (Result, error) {
	result := Result{NodeIDs: []string{root}, Edges: make([]Edge, 0)}
	if root == "" || limits.Hops < 1 || limits.Nodes < 1 || limits.Outgoing < 1 {
		return result, context.Canceled
	}
	seen := map[string]bool{root: true}
	queue := []entry{{root, 0}}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		current := queue[0]
		queue = queue[1:]
		if current.depth >= limits.Hops {
			outgoing, err := fetch(ctx, current.id, 1)
			if err != nil {
				return result, err
			}
			if len(outgoing) > 0 {
				result.Truncated = true
			}
			continue
		}
		outgoing, err := fetch(ctx, current.id, limits.Outgoing+1)
		if err != nil {
			return result, err
		}
		if len(outgoing) > limits.Outgoing {
			result.Truncated = true
			outgoing = outgoing[:limits.Outgoing]
		}
		for _, edge := range outgoing {
			if edge.FromID != current.id || edge.ToID == "" {
				continue
			}
			if !seen[edge.ToID] {
				if len(result.NodeIDs) >= limits.Nodes {
					result.Truncated = true
					continue
				}
				seen[edge.ToID] = true
				result.NodeIDs = append(result.NodeIDs, edge.ToID)
				queue = append(queue, entry{edge.ToID, current.depth + 1})
			}
			result.Edges = append(result.Edges, edge)
		}
	}
	return result, nil
}
