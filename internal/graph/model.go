package graph

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const (
	MaxNodes   = 500
	MaxEdges   = 1600
	MaxBudget  = 12
	MaxResults = 80
)

type Reference struct {
	Repository string `json:"repository" yaml:"repository"`
	Path       string `json:"path" yaml:"path"`
	Anchor     string `json:"anchor,omitempty" yaml:"anchor,omitempty"`
}

type Node struct {
	ID          string     `json:"id" yaml:"id"`
	Kind        string     `json:"kind" yaml:"kind"`
	Label       string     `json:"label" yaml:"label"`
	Parent      string     `json:"parent,omitempty" yaml:"parent,omitempty"`
	Description string     `json:"description,omitempty" yaml:"description,omitempty"`
	Rule        string     `json:"rule,omitempty" yaml:"rule,omitempty"`
	Reference   *Reference `json:"reference,omitempty" yaml:"reference,omitempty"`
}

type Edge struct {
	ID     string `json:"id" yaml:"id"`
	From   string `json:"from" yaml:"from"`
	To     string `json:"to" yaml:"to"`
	Kind   string `json:"kind" yaml:"kind"`
	Anchor string `json:"anchor,omitempty" yaml:"anchor,omitempty"`
}

type Snapshot struct {
	Revision int    `json:"revision" yaml:"revision"`
	Nodes    []Node `json:"nodes" yaml:"nodes"`
	Edges    []Edge `json:"edges" yaml:"edges"`
}

type Patch struct {
	Nodes       []Node   `json:"upsert_nodes,omitempty"`
	Edges       []Edge   `json:"upsert_edges,omitempty"`
	RemoveNodes []string `json:"remove_nodes,omitempty"`
	RemoveEdges []string `json:"remove_edges,omitempty"`
}

func Empty() Snapshot {
	return Snapshot{Nodes: []Node{}, Edges: []Edge{}}
}

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

func ValidID(value string) bool {
	return identifier.MatchString(value)
}

func cleanText(value string, max int) bool {
	return len(value) <= max && !strings.ContainsFunc(value, unicode.IsControl)
}

func validReference(ref Reference) bool {
	return ValidID(ref.Repository) && len(ref.Path) <= 256 && cleanText(ref.Path, 256) &&
		ref.Path != "." && fs.ValidPath(ref.Path) && path.Clean(ref.Path) == ref.Path &&
		!strings.ContainsAny(ref.Path, "\\:%?#") && cleanText(ref.Anchor, 180)
}

func edgeCost(kind string) int {
	switch kind {
	case "defined_in", "measured_from":
		return 1
	case "depends_on", "affected_by":
		return 2
	default:
		return 0
	}
}

// Validate checks hierarchy and semantic edges independently. References are
// client-resolved coordinates, so the graph never opens a referenced file.
func Validate(s Snapshot) error {
	if len(s.Nodes) > MaxNodes || len(s.Edges) > MaxEdges {
		return fmt.Errorf("graph exceeds the limit of %d nodes or %d relations", MaxNodes, MaxEdges)
	}
	nodes := make(map[string]Node, len(s.Nodes))
	refs := make(map[[2]string]bool)
	for _, n := range s.Nodes {
		if !ValidID(n.ID) || strings.TrimSpace(n.Label) == "" || !cleanText(n.Label, 120) ||
			!cleanText(n.Description, 1600) || !cleanText(n.Rule, 800) {
			return fmt.Errorf("invalid node identifier, label or text")
		}
		if _, ok := nodes[n.ID]; ok {
			return fmt.Errorf("duplicate node %q", n.ID)
		}
		switch n.Kind {
		case "domain", "concept", "metric":
			if n.Reference != nil {
				return fmt.Errorf("only source nodes may have a reference (%s)", n.ID)
			}
		case "source":
			if n.Reference == nil || !validReference(*n.Reference) {
				return fmt.Errorf("source %s needs a repository alias and relative file path", n.ID)
			}
			key := [2]string{n.Reference.Repository, n.Reference.Path}
			if refs[key] {
				return fmt.Errorf("source %s duplicates an existing file", n.ID)
			}
			refs[key] = true
		default:
			return fmt.Errorf("unsupported node kind on %s", n.ID)
		}
		nodes[n.ID] = n
	}

	parents := make(map[string][]string)
	for _, n := range s.Nodes {
		if n.Parent == "" {
			if n.Kind != "domain" {
				return fmt.Errorf("%s needs a parent domain", n.ID)
			}
			continue
		}
		if nodes[n.Parent].Kind != "domain" {
			return fmt.Errorf("parent of %s must be a domain in this space", n.ID)
		}
		parents[n.Parent] = append(parents[n.Parent], n.ID)
	}
	if hasCycle(s.Nodes, parents) {
		return fmt.Errorf("domain hierarchy contains a cycle")
	}

	relations := make(map[string][]string)
	ids := make(map[string]bool)
	keys := make(map[[4]string]bool)
	for _, e := range s.Edges {
		from, fromOK := nodes[e.From]
		to, toOK := nodes[e.To]
		if !ValidID(e.ID) || ids[e.ID] || !fromOK || !toOK || e.From == e.To ||
			from.Kind == "source" || edgeCost(e.Kind) == 0 || !cleanText(e.Anchor, 180) {
			return fmt.Errorf("invalid or duplicate relation %q", e.ID)
		}
		isEvidence := e.Kind == "defined_in" || e.Kind == "measured_from"
		if isEvidence != (to.Kind == "source") || (e.Anchor != "" && !isEvidence) {
			return fmt.Errorf("relation %s does not match its target kind or anchor", e.ID)
		}
		key := [4]string{e.From, e.To, e.Kind, e.Anchor}
		if keys[key] {
			return fmt.Errorf("relation %s duplicates an existing relationship", e.ID)
		}
		ids[e.ID], keys[key] = true, true
		relations[e.From] = append(relations[e.From], e.To)
	}
	if hasCycle(s.Nodes, relations) {
		return fmt.Errorf("semantic relations contain a cycle")
	}
	return nil
}

func hasCycle(nodes []Node, adjacency map[string][]string) bool {
	color := make(map[string]int, len(nodes))
	var visit func(string) bool
	visit = func(id string) bool {
		if color[id] == 1 {
			return true
		}
		if color[id] == 2 {
			return false
		}
		color[id] = 1
		for _, child := range adjacency[id] {
			if visit(child) {
				return true
			}
		}
		color[id] = 2
		return false
	}
	return slices.ContainsFunc(nodes, func(n Node) bool { return visit(n.ID) })
}

// Apply materializes a complete candidate against an immutable revision.
func Apply(base Snapshot, patch Patch) (Snapshot, error) {
	if len(patch.Nodes)+len(patch.Edges)+len(patch.RemoveNodes)+len(patch.RemoveEdges) == 0 {
		return Snapshot{}, fmt.Errorf("proposal has no changes")
	}
	nodes, err := merge(base.Nodes, patch.Nodes, patch.RemoveNodes, func(n Node) string { return n.ID })
	if err != nil {
		return Snapshot{}, err
	}
	edges, err := merge(base.Edges, patch.Edges, patch.RemoveEdges, func(e Edge) string { return e.ID })
	if err != nil {
		return Snapshot{}, err
	}
	candidate := Snapshot{Revision: base.Revision + 1, Nodes: nodes, Edges: edges}
	return candidate, Validate(candidate)
}

func merge[T any](base, upsert []T, remove []string, id func(T) string) ([]T, error) {
	entries := make(map[string]T, len(base)+len(upsert))
	touched := make(map[string]bool)
	for _, value := range base {
		entries[id(value)] = value
	}
	for _, key := range remove {
		if _, ok := entries[key]; !ok {
			return nil, fmt.Errorf("cannot remove missing entry %q", key)
		}
		delete(entries, key)
		touched[key] = true
	}
	for _, value := range upsert {
		key := id(value)
		if touched[key] {
			return nil, fmt.Errorf("multiple operations target %q", key)
		}
		entries[key] = value
		touched[key] = true
	}
	result := make([]T, 0, len(entries))
	for _, value := range entries {
		result = append(result, value)
	}
	slices.SortFunc(result, func(a, b T) int { return strings.Compare(id(a), id(b)) })
	return result, nil
}
