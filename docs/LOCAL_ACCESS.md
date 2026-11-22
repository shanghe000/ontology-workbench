# Local access and MCP

The server binds to IPv4 loopback. The in-memory demo has reviewer controls for local exploration. A persistent workbench requires a separate reviewer token. Optional machine credentials are scoped to a workspace and have read or proposal capabilities.

## Generate a private local config

This command creates a mode-0600 file in your working directory. It refuses to overwrite an existing file. Use `parcel` for the demo or `main` for your persistent workspace.

```sh
python3 - <<'PY'
import json, os, secrets
os.umask(0o077)
config = {
    "review_token": secrets.token_urlsafe(32),
    "spaces": {
        "parcel": {"read": secrets.token_urlsafe(32), "propose": secrets.token_urlsafe(32)}
    }
}
with open("workbench.access.json", "x", encoding="utf-8") as f:
    json.dump(config, f, indent=2)
PY
WORKBENCH_ACCESS_FILE=./workbench.access.json go run ./cmd/workbench -demo
```

Open the file locally to retrieve the reviewer token once and enter it in the browser. For persistence, change the `spaces` entry to `main` and start:

```sh
WORKBENCH_ACCESS_FILE=./workbench.access.json go run ./cmd/workbench -data ./workbench.sqlite
```

The default persistent workspace begins empty. Additional entries in `spaces` initialize additional independent workspaces on startup. A configured machine token is valid only for its workspace. Omit `propose` where agents only need read access. Restart after replacing credentials.

To seed an empty workspace through the UI, create a proposal at `r0`, use the `nodes` and `edges` in the [example graph](../examples/parcel/graph.json) as `upsert_nodes` and `upsert_edges`, then review it. The built-in source excerpts are available in demo mode. A persistent workspace refers to files via its chosen repository aliases.

## Read evidence with an MCP client

Use Streamable HTTP at `http://127.0.0.1:8080/mcp/parcel` and set an `Authorization: Bearer …` header using the corresponding read or proposal credential. The transport is stateless. For an example that reads the token directly from its file, run:

```sh
python3 examples/trace_mcp.py --access workbench.access.json --space parcel --node on-time
```

| Tool | Scope | Arguments |
| --- | --- | --- |
| `read_graph` | Read or propose | Optional `revision`. Defaults to the latest published version. |
| `trace_evidence` | Read or propose | `node`, optional `budget` and `revision`. |
| `submit_proposal` | Propose | `request_id`, `title`, `base_revision` and `patch`. |

Approve or reject proposals through the reviewer UI. The workbench uses a local owner credential; it does not provide an internet-facing identity service.

## Enable DeepSeek drafting

Set `DEEPSEEK_API_KEY` in the server process environment using your preferred secret storage. The default model is `deepseek-flash`, and `DEEPSEEK_MODEL` can select another supported model. Restart and use **New proposal → Draft with DeepSeek**.

The form asks you to confirm the instruction and published graph metadata before sending them to [DeepSeek](https://api-docs.deepseek.com/api/create-chat-completion). The application sends graph labels, relationships and references as metadata; it does not open or upload the referenced files. Review the suggested JSON patch and submit it through the same approval flow.
