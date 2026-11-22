// Package examples contains the fictional parcel operations dataset.
package examples

import (
	"embed"
	"encoding/json"
	"io/fs"

	"github.com/shanghe000/ontology-workbench/internal/graph"
)

//go:embed parcel/graph.json parcel/weekend-patch.json parcel/sources/*.md
var assets embed.FS

func Parcel() (graph.Snapshot, graph.Patch, error) {
	var snapshot graph.Snapshot
	var patch graph.Patch
	data, err := assets.ReadFile("parcel/graph.json")
	if err != nil {
		return snapshot, patch, err
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, patch, err
	}
	data, err = assets.ReadFile("parcel/weekend-patch.json")
	if err != nil {
		return snapshot, patch, err
	}
	err = json.Unmarshal(data, &patch)
	return snapshot, patch, err
}

func Sources() fs.FS {
	sources, err := fs.Sub(assets, "parcel/sources")
	if err != nil {
		panic(err)
	}
	return sources
}
