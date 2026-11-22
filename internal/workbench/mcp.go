package workbench

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shanghe000/ontology-workbench/internal/catalog"
	"github.com/shanghe000/ontology-workbench/internal/graph"
)

func (a api) mcp() http.Handler {
	type routes struct {
		read, propose http.Handler
	}
	handlers := make(map[string]routes, len(a.options.Access.spaces))
	for space := range a.options.Access.spaces {
		handlers[space] = routes{a.mcpServer(space, false), a.mcpServer(space, true)}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		space := r.PathValue("space")
		pair := handlers[space]
		secrets := a.options.Access.spaces[space]
		switch {
		case secrets.read.matches(r):
			pair.read.ServeHTTP(w, r)
		case secrets.propose.matches(r):
			pair.propose.ServeHTTP(w, r)
		default:
			w.Header().Set("WWW-Authenticate", "Bearer")
			respond(w, http.StatusUnauthorized, map[string]string{"error": "token required for this workspace"})
		}
	})
}

func (a api) mcpServer(space string, canPropose bool) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "ontology-workbench", Version: "0.1.0"}, nil)

	type readArgs struct {
		Revision *int `json:"revision,omitempty" jsonschema:"Published revision number. Omit to read the head."`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "read_graph", Description: "Read a published workspace graph by revision."},
		func(ctx context.Context, _ *mcp.CallToolRequest, arg readArgs) (*mcp.CallToolResult, graph.Snapshot, error) {
			snapshot, err := a.store.Graph(ctx, space, arg.Revision)
			return nil, snapshot, err
		})

	type traceArgs struct {
		Node     string `json:"node" jsonschema:"Stable ID of an existing node."`
		Budget   *int   `json:"budget,omitempty" jsonschema:"Traversal cost budget between 0 and 12. Defaults to 4."`
		Revision *int   `json:"revision,omitempty" jsonschema:"Published revision number. Omit to trace the head."`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "trace_evidence", Description: "Follow directed relations in the published graph and return the cheapest evidence paths."},
		func(ctx context.Context, _ *mcp.CallToolRequest, arg traceArgs) (*mcp.CallToolResult, graph.Trace, error) {
			snapshot, err := a.store.Graph(ctx, space, arg.Revision)
			if err != nil {
				return nil, graph.Trace{}, err
			}
			budget := 4
			if arg.Budget != nil {
				budget = *arg.Budget
			}
			result, err := graph.Explore(snapshot, arg.Node, budget, 40)
			return nil, result, err
		})

	if canPropose {
		type receipt struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		mcp.AddTool(server, &mcp.Tool{Name: "submit_proposal", Description: "Queue a validated patch for human review. Retry with the same request ID and identical content."},
			func(ctx context.Context, _ *mcp.CallToolRequest, arg catalog.Submission) (*mcp.CallToolResult, receipt, error) {
				p, err := a.store.Submit(ctx, space, arg)
				return nil, receipt{p.ID, p.Status}, err
			})
	}
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
