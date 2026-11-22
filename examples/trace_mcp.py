#!/usr/bin/env python3
"""Read a published evidence trace from the local MCP demo."""
import argparse
import json
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request


class LocalOnly(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--access", type=Path, required=True)
    parser.add_argument("--space", default="parcel")
    parser.add_argument("--node", default="on-time")
    parser.add_argument("--budget", type=int, default=4)
    parser.add_argument("--port", type=int, default=8080)
    args = parser.parse_args()
    try:
        token = json.loads(args.access.read_text())["spaces"][args.space]["read"]
    except (OSError, ValueError, KeyError, TypeError):
        parser.error("could not read the workspace's read token from the access file")
    endpoint = f"http://127.0.0.1:{args.port}/mcp/{urllib.parse.quote(args.space, safe='')}"
    headers = {
        "Authorization": "Bearer " + token,
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    }
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), LocalOnly())

    def send(method, params, message_id=None):
        message = {"jsonrpc": "2.0", "method": method, "params": params}
        if message_id is not None:
            message["id"] = message_id
        request = urllib.request.Request(endpoint, data=json.dumps(message).encode(), headers=headers)
        # Avoid forwarding the token through a machine-wide proxy.
        with opener.open(request, timeout=8) as response:
            result = json.load(response) if message_id is not None else {}
        if "error" in result:
            raise ValueError(result["error"].get("message", "MCP error"))
        return result.get("result")

    try:
        connection = send("initialize", {
            "protocolVersion": "2025-06-18",
            "capabilities": {},
            "clientInfo": {"name": "workbench-example", "version": "1"},
        }, 1)
        headers["MCP-Protocol-Version"] = connection["protocolVersion"]
        send("notifications/initialized", {})
        result = send("tools/call", {
            "name": "trace_evidence",
            "arguments": {"node": args.node, "budget": args.budget},
        }, 2)
        if result.get("isError"):
            raise ValueError(result["content"][0]["text"])
        print(json.dumps(result["structuredContent"], ensure_ascii=False, indent=2))
    except (urllib.error.URLError, OSError, ValueError, KeyError) as exc:
        parser.exit(1, f"{exc}\n")


if __name__ == "__main__":
    main()
