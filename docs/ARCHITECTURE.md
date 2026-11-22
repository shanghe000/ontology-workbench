# Architecture and graph model

The Go executable embeds a browser UI. The local HTTP API and stateless [MCP Streamable HTTP](https://modelcontextprotocol.io/docs/develop/build-server) both load immutable SQLite revisions and call the graph package. Rendering and ancestor grouping are browser concerns. The browser requests a trace pinned to its current revision.

| Layer | Responsibility |
| --- | --- |
| `internal/graph` | Validate containment, directed relations and source coordinates. Apply a typed patch and compute budgeted shortest paths. |
| `internal/catalog` | Partition revisions and proposals by workspace. Deduplicate submissions and advance a workspace head transactionally. |
| `internal/workbench` | Serve the browser, resolve local credentials, expose MCP tools, and validate optional model drafts. |
| `web` | Evidence flow, ontology layers, reference inspector and proposal review. |
| `examples/parcel` | Fictional graph, review patch and the material they reference. |

## Nodes and relationships

[Example graph JSON](../examples/parcel/graph.json) · [Example patch](../examples/parcel/weekend-patch.json)

- **Domain** groups a branch of the ontology. A root entry is a domain. Other entries have a domain parent. The parent hierarchy and semantic graph are validated separately.
- **Concept** represents a process, dependency or business mechanism.
- **Metric** names a measure and can carry its formula or rule.
- **Source** identifies a unique file within a repository alias. A reference is a coordinate for the client to resolve. Source nodes terminate traversal.

| Relation | Meaning | Budget cost |
| --- | --- | --- |
| `defined_in` | Links a node to the source that defines it. | 1 |
| `measured_from` | Links an observation or measure to its source. | 1 |
| `depends_on` | Links a node to a prerequisite. | 2 |
| `affected_by` | Links a measure or process to a possible influence. | 2 |

Different edges can target different headings in one shared source. `affected_by` records an ontology relationship. Establishing an empirical causal effect requires separate evidence.

The traversal keeps the cheapest known route to each node and stops at the requested cost or result limit. Its frontier identifies outgoing targets that were not included. Use a larger budget or trace one of those targets against the same revision. A workspace can contain up to 500 nodes and 1,600 edges in this implementation.

## Proposal lifecycle

A submission contains `request_id`, `title`, `base_revision` and `patch`. The patch carries `upsert_nodes`, `upsert_edges`, `remove_nodes` and `remove_edges`. Upsert entries specify the full node or edge. Removing a node also requires removing its remaining references in the same patch.

The store validates and records a pending proposal against the requested head. Identical retries return the existing receipt. An approved proposal creates the next immutable revision and updates the review status within one transaction. Competing approvals against a stale base require a new proposal; they cannot silently replay onto the current revision. Historical graphs remain readable by revision.

## Model drafting

If configured, the server requests [DeepSeek JSON Output](https://api-docs.deepseek.com/guides/json_mode) using the published graph metadata and an explicit instruction. It rejects truncated or invalid JSON, deletions, changes to source records and unknown anchors. The resulting patch can be edited in the workbench before submission. Reviewers remain responsible for the meaning and evidence behind each change.
