package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/shanghe000/ontology-workbench/examples"
	"github.com/shanghe000/ontology-workbench/internal/catalog"
	"github.com/shanghe000/ontology-workbench/internal/graph"
	"github.com/shanghe000/ontology-workbench/internal/workbench"
	"github.com/shanghe000/ontology-workbench/web"
)

func main() {
	demo := flag.Bool("demo", false, "start an in-memory sandbox with the fictional parcel example")
	filename := flag.String("data", "workbench.sqlite", "path to the local SQLite database")
	port := flag.Int("port", 8080, "loopback port (0 allocates a free port)")
	flag.Parse()
	if flag.NArg() != 0 {
		log.Fatal("unexpected positional arguments; run with -help")
	}
	if err := run(*demo, *filename, *port); err != nil {
		log.Fatal(err)
	}
}

func run(demo bool, filename string, port int) error {
	access, err := workbench.LoadAccess(os.Getenv("WORKBENCH_ACCESS_FILE"), demo)
	if err != nil {
		return err
	}
	if demo {
		filename = ":memory:"
	} else {
		if filename == ":memory:" || filename == "" {
			return fmt.Errorf("-data must be a SQLite file for persistent mode")
		}
		file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("cannot prepare the SQLite file: %w", err)
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	store, err := catalog.Open(filename)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()
	if demo {
		snapshot, patch, err := examples.Parcel()
		if err != nil {
			return err
		}
		if err := store.EnsureSpace(ctx, "parcel", "Parcel operations", snapshot); err != nil {
			return err
		}
		if err := store.EnsureSpace(ctx, "sandbox", "Empty workspace", graph.Empty()); err != nil {
			return err
		}
		if _, err := store.Submit(ctx, "parcel", catalog.Submission{RequestID: "weekend-cutoff", Title: "Clarify weekend handoffs",
			BaseRevision: &snapshot.Revision, Patch: patch}); err != nil {
			return err
		}
	} else if err := store.EnsureSpace(ctx, "main", "Main workspace", graph.Empty()); err != nil {
		return err
	}
	for _, id := range access.SpaceIDs() {
		if err := store.EnsureSpace(ctx, id, id, graph.Empty()); err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	planner := workbench.DeepSeek(os.Getenv("DEEPSEEK_API_KEY"), os.Getenv("DEEPSEEK_MODEL"))
	handler := workbench.Handler(store, workbench.Options{Demo: demo, Access: access, Planner: planner, Assets: web.Assets})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 12 * time.Second,
		WriteTimeout: 45 * time.Second, IdleTimeout: 30 * time.Second}
	log.Printf("Ontology Workbench is listening at http://%s", listener.Addr())
	return srv.Serve(listener)
}
