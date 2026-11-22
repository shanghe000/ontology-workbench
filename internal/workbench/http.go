package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/shanghe000/ontology-workbench/examples"
	"github.com/shanghe000/ontology-workbench/internal/catalog"
	"github.com/shanghe000/ontology-workbench/internal/graph"
	"go.yaml.in/yaml/v3"
)

const maxBody = 2 << 20

type Options struct {
	Demo    bool
	Access  Access
	Planner *Planner
	Assets  fs.FS
}

type api struct {
	store   *catalog.Store
	options Options
}

func Handler(store *catalog.Store, options Options) http.Handler {
	app := api{store: store, options: options}
	mux := http.NewServeMux()
	web := http.NewServeMux()
	web.HandleFunc("GET /api/spaces", app.spaces)
	web.HandleFunc("GET /api/spaces/{space}/graph", app.graph)
	web.HandleFunc("GET /api/spaces/{space}/trace", app.trace)
	web.HandleFunc("GET /api/spaces/{space}/proposals", app.proposals)
	web.HandleFunc("POST /api/spaces/{space}/proposals", app.submit)
	web.HandleFunc("GET /api/spaces/{space}/proposals/{proposal}", app.preview)
	web.HandleFunc("POST /api/spaces/{space}/proposals/{proposal}/review", app.review)
	web.HandleFunc("POST /api/spaces/{space}/drafts", app.draft)
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (!options.Demo || options.Access.review != (tokenHash{})) && !options.Access.review.matches(r) {
			respond(w, http.StatusUnauthorized, map[string]string{"error": "review token required"})
			return
		}
		web.ServeHTTP(w, r)
	}))
	if options.Demo {
		mux.Handle("GET /example/", http.StripPrefix("/example/", http.FileServerFS(examples.Sources())))
	}
	mux.Handle("/mcp/{space}", app.mcp())
	assets := http.FileServerFS(options.Assets)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		assets.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if !localRequest(r) {
			respond(w, http.StatusForbidden, map[string]string{"error": "loopback origin required"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		mux.ServeHTTP(w, r)
	})
}

func (a api) spaces(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.Spaces(r.Context())
	if err != nil {
		failed(w, err)
		return
	}
	respond(w, http.StatusOK, struct {
		Spaces    []catalog.Space `json:"spaces"`
		Demo      bool            `json:"demo"`
		Assistant bool            `json:"assistant"`
	}{items, a.options.Demo, a.options.Planner != nil})
}

func (a api) graph(w http.ResponseWriter, r *http.Request) {
	revision, err := requestedRevision(r)
	if err != nil {
		invalid(w, err.Error())
		return
	}
	snapshot, err := a.store.Graph(r.Context(), r.PathValue("space"), revision)
	if err != nil {
		failed(w, err)
		return
	}
	if r.URL.Query().Get("format") == "yaml" {
		raw, err := yaml.Marshal(snapshot)
		if err != nil {
			failed(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="ontology.yaml"`)
		w.Write(raw)
		return
	}
	respond(w, http.StatusOK, snapshot)
}

func (a api) trace(w http.ResponseWriter, r *http.Request) {
	budget := 4
	if value := r.URL.Query().Get("budget"); value != "" {
		v, err := strconv.Atoi(value)
		if err != nil {
			invalid(w, "budget must be an integer")
			return
		}
		budget = v
	}
	revision, err := requestedRevision(r)
	if err != nil {
		invalid(w, err.Error())
		return
	}
	snapshot, err := a.store.Graph(r.Context(), r.PathValue("space"), revision)
	if err != nil {
		failed(w, err)
		return
	}
	result, err := graph.Explore(snapshot, r.URL.Query().Get("node"), budget, 40)
	if err != nil {
		invalid(w, err.Error())
		return
	}
	respond(w, http.StatusOK, result)
}

func requestedRevision(r *http.Request) (*int, error) {
	value := r.URL.Query().Get("revision")
	if value == "" {
		return nil, nil
	}
	revision, err := strconv.Atoi(value)
	if err != nil || revision < 0 {
		return nil, fmt.Errorf("revision must be a non-negative integer")
	}
	return &revision, nil
}

func (a api) proposals(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.Proposals(r.Context(), r.PathValue("space"))
	if err != nil {
		failed(w, err)
		return
	}
	respond(w, http.StatusOK, items)
}

func (a api) submit(w http.ResponseWriter, r *http.Request) {
	input, err := decode[catalog.Submission](r)
	if err != nil {
		invalid(w, err.Error())
		return
	}
	p, err := a.store.Submit(r.Context(), r.PathValue("space"), input)
	if err != nil {
		failed(w, err)
		return
	}
	respond(w, http.StatusAccepted, p)
}

func (a api) preview(w http.ResponseWriter, r *http.Request) {
	p, candidate, err := a.store.Preview(r.Context(), r.PathValue("space"), r.PathValue("proposal"))
	if err != nil {
		failed(w, err)
		return
	}
	respond(w, http.StatusOK, struct {
		Proposal  catalog.Proposal `json:"proposal"`
		Candidate graph.Snapshot   `json:"candidate"`
	}{p, candidate})
}

func (a api) review(w http.ResponseWriter, r *http.Request) {
	input, err := decode[struct {
		Decision string `json:"decision"`
	}](r)
	if err != nil {
		invalid(w, err.Error())
		return
	}
	p, err := a.store.Review(r.Context(), r.PathValue("space"), r.PathValue("proposal"), input.Decision)
	if err != nil {
		failed(w, err)
		return
	}
	respond(w, http.StatusOK, p)
}

func decode[T any](r *http.Request) (T, error) {
	var value T
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&value); err != nil {
		return value, fmt.Errorf("expected valid JSON matching the request schema")
	}
	if dec.Decode(new(any)) != io.EOF {
		return value, fmt.Errorf("expected exactly one JSON object")
	}
	return value, nil
}

func respond(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(value)
}

func invalid(w http.ResponseWriter, message string) {
	respond(w, http.StatusBadRequest, map[string]string{"error": message})
}

func failed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, catalog.ErrMissing):
		respond(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, catalog.ErrConflict):
		respond(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, catalog.ErrInvalid):
		invalid(w, err.Error())
	default:
		respond(w, http.StatusInternalServerError, map[string]string{"error": "operation could not be completed"})
	}
}
