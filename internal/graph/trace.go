package graph

import (
	"container/heap"
	"fmt"
	"slices"
	"strings"
)

type Step struct {
	Node Node   `json:"node"`
	Cost int    `json:"cost"`
	Via  string `json:"via,omitempty"`
}

type Trace struct {
	Revision int      `json:"revision"`
	Steps    []Step   `json:"steps"`
	Edges    []Edge   `json:"edges"`
	Frontier []string `json:"frontier"`
}

type visit struct {
	id, via string
	cost    int
}

type priorityQueue []visit

func (q priorityQueue) Len() int { return len(q) }
func (q priorityQueue) Less(i, j int) bool {
	if q[i].cost != q[j].cost {
		return q[i].cost < q[j].cost
	}
	return q[i].id < q[j].id
}
func (q priorityQueue) Swap(i, j int)   { q[i], q[j] = q[j], q[i] }
func (q *priorityQueue) Push(value any) { *q = append(*q, value.(visit)) }
func (q *priorityQueue) Pop() any {
	last := (*q)[len(*q)-1]
	*q = (*q)[:len(*q)-1]
	return last
}

// Explore follows declared edge direction. Evidence edges cost one unit,
// business relations cost two. Results are monotonic with the budget.
func Explore(s Snapshot, root string, budget, limit int) (Trace, error) {
	if budget < 0 || budget > MaxBudget || limit < 1 || limit > MaxResults {
		return Trace{}, fmt.Errorf("budget must be 0–%d and limit 1–%d", MaxBudget, MaxResults)
	}
	nodes := make(map[string]Node, len(s.Nodes))
	for _, n := range s.Nodes {
		nodes[n.ID] = n
	}
	if _, ok := nodes[root]; !ok {
		return Trace{}, fmt.Errorf("root node does not exist in this revision")
	}
	adj := make(map[string][]Edge)
	for _, e := range s.Edges {
		adj[e.From] = append(adj[e.From], e)
	}
	for _, outgoing := range adj {
		slices.SortFunc(outgoing, func(a, b Edge) int { return strings.Compare(a.ID, b.ID) })
	}
	result := Trace{Revision: s.Revision, Steps: []Step{}, Edges: []Edge{}, Frontier: []string{}}
	queue := &priorityQueue{{id: root}}
	best := map[string]int{root: 0}
	reached := map[string]bool{}
	for queue.Len() != 0 && len(result.Steps) < limit {
		item := heap.Pop(queue).(visit)
		if reached[item.id] || best[item.id] != item.cost {
			continue
		}
		reached[item.id] = true
		result.Steps = append(result.Steps, Step{Node: nodes[item.id], Cost: item.cost, Via: item.via})
		for _, e := range adj[item.id] {
			cost := item.cost + edgeCost(e.Kind)
			previous, seen := best[e.To]
			if cost <= budget && (!seen || cost < previous) {
				best[e.To] = cost
				heap.Push(queue, visit{id: e.To, via: e.ID, cost: cost})
			}
		}
	}
	boundary := map[string]bool{}
	for _, e := range s.Edges {
		if !reached[e.From] {
			continue
		}
		if !reached[e.To] {
			boundary[e.To] = true
		} else if best[e.From]+edgeCost(e.Kind) <= budget {
			result.Edges = append(result.Edges, e)
		}
	}
	for id := range boundary {
		result.Frontier = append(result.Frontier, id)
	}
	slices.Sort(result.Frontier)
	slices.SortFunc(result.Edges, func(a, b Edge) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}
