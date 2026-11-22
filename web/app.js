import {attachDiagram} from "./graph.js";

const $ = id => document.getElementById(id);
const state = {
  spaces: [], space: "", demo: false, assistant: false, token: "",
  graph: null, review: null, proposals: [], view: "flow", selected: "", trace: null,
  loading: 0, tracing: 0, draft: null
};
const renderDiagram = attachDiagram($("diagram"), selectNode);

function node(tag, text, className) {
  const element = document.createElement(tag);
  if (text !== undefined) element.textContent = text;
  if (className) element.className = className;
  return element;
}

function button(text, action, className) {
  const b = node("button", text, className);
  b.type = "button";
  b.addEventListener("click", action);
  return b;
}

function notice(message, error = false) {
  const p = $("notice");
  p.hidden = !message;
  p.classList.toggle("error", error);
  p.textContent = message;
}

function showError(error) {
  notice(error.message, true);
}

async function api(path, options = {}) {
  const headers = {Accept: "application/json"};
  if (state.token) headers.Authorization = "Bearer " + state.token;
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch("/api" + path, {...options, headers});
  if (!res.ok) {
    const detail = await res.json().catch(() => ({}));
    const error = new Error(detail.error || "Request failed (" + res.status + ")");
    error.status = res.status;
    throw error;
  }
  return options.file ? res.blob() : res.json();
}

function spacePath(space = state.space) {
  return "/spaces/" + encodeURIComponent(space);
}

function currentGraph() {
  return state.review?.candidate || state.graph;
}

async function connect() {
  const catalog = await api("/spaces");
  state.spaces = catalog.spaces;
  state.demo = catalog.demo;
  state.assistant = catalog.assistant;
  $("spaces").replaceChildren(...state.spaces.map(s => {
    const option = node("option", s.label);
    option.value = s.id;
    return option;
  }));
  await loadSpace(state.spaces[0]?.id || "");
}

async function loadSpace(id) {
  if (!id) return;
  const turn = ++state.loading;
  state.tracing++;
  $("spaces").disabled = true;
  try {
    const path = spacePath(id);
    const [graph, proposals] = await Promise.all([api(path + "/graph"), api(path + "/proposals")]);
    if (state.loading !== turn) return;
    state.space = id;
    state.graph = graph;
    state.proposals = proposals;
    state.review = null;
    state.trace = null;
    if (!graph.nodes.some(n => n.id === state.selected)) {
      state.selected = graph.nodes.find(n => n.kind === "metric")?.id || graph.nodes[0]?.id || "";
    }
    $("spaces").value = id;
    draw();
    await traceNode();
  } finally {
    if (state.loading === turn) $("spaces").disabled = false;
  }
}

function draw() {
  const g = currentGraph();
  if (!g) return;
  const p = state.review?.proposal;
  const marks = p?.patch || {};
  const markedNodes = new Set((marks.upsert_nodes || []).map(n => n.id));
  const markedEdges = new Set((marks.upsert_edges || []).map(e => e.id));
  $("space-title").textContent = state.spaces.find(s => s.id === state.space)?.label || state.space;
  $("workspace-note").textContent = state.demo
    ? "Fictional parcel data for tracing the evidence behind a metric."
    : "Explore metrics, their dependencies, and the documents behind them.";
  $("revision").textContent = "Published r" + state.graph.revision;
  $("pending-count").textContent = state.proposals.filter(p => p.status === "pending").length || "";
  $("export").disabled = false;
  $("draft").disabled = Boolean(p);
  $("review-banner").hidden = !p;
  if (p) {
    $("review-title").textContent = p.title;
    const stale = p.base_revision !== state.graph.revision;
    $("review-summary").textContent = "Proposed r" + g.revision + " · " + markedNodes.size + " node / " + markedEdges.size
      + " relation upserts" + (stale ? " · Published since this proposal was drafted" : "");
    $("approve").disabled = p.status !== "pending" || stale;
    $("reject").disabled = p.status !== "pending";
  }
  $("empty-graph").hidden = g.nodes.length > 0;
  $("budget").disabled = Boolean(p) || !state.selected;
  $("graph-count").textContent = g.nodes.length + " nodes · " + g.edges.length + " relations";

  renderIndex(g);
  renderInspector(g);
  renderDiagram({snapshot: g, selected: state.selected, trace: state.trace, view: state.view, markedNodes, markedEdges});
}

function renderIndex(g) {
  const list = $("node-list");
  const focused = document.activeElement?.dataset.indexId;
  list.replaceChildren();
  const byParent = new Map();
  for (const item of g.nodes) {
    const group = byParent.get(item.parent) || [];
    group.push(item);
    byParent.set(item.parent, group);
  }
  function add(parent) {
    for (const item of byParent.get(parent) || []) {
      const b = button(item.label, () => {
        if (item.kind === "domain") setView("layers");
        selectNode(item.id);
      }, "index-node");
      b.dataset.indexId = item.id;
      b.setAttribute("aria-current", String(item.id === state.selected));
      if (item.kind === "domain") {
        const h = node("h3", undefined, "index-domain");
        h.append(b);
        list.append(h);
        add(item.id);
      } else if (item.kind === "metric" || item.kind === "concept") {
        list.append(b);
      }
    }
  }
  add(undefined);
  if (!list.children.length) list.append(node("p", "No domains yet.", "muted"));
  if (focused) Array.from(list.querySelectorAll("button")).find(b => b.dataset.indexId === focused)?.focus({preventScroll: true});
}

const sampleFiles = new Set(["delivery-policy.md", "dispatch-playbook.md", "event-dictionary.md"]);
function sourceFile(ref) {
  if (!state.demo || ref.repository !== "parcel-demo" || !ref.path.startsWith("sources/")) return "";
  const file = ref.path.slice("sources/".length);
  return sampleFiles.has(file) ? file : "";
}

async function showSource(source, anchor) {
  const file = sourceFile(source.reference);
  if (!file) return;
  const res = await fetch("/example/" + file);
  if (!res.ok) throw new Error("The sample document could not be loaded");
  const raw = await res.text();
  const sections = raw.split(/^## /m);
  const selected = anchor && sections.find(s => s.split("\n")[0].trim().toLowerCase().replace(/[^a-z0-9]+/g, "-") === anchor);
  const location = source.reference.repository + " / " + source.reference.path + (anchor ? "#" + anchor : "");
  openText(source.label, location, selected ? selected.trim() : raw.trim());
}

function renderInspector(g) {
  const panel = $("inspector");
  const active = g.nodes.find(n => n.id === state.selected);
  panel.replaceChildren();
  if (!active) {
    panel.append(node("p", "Select a graph node to inspect its definition and sources."));
    return;
  }
  panel.append(node("div", active.kind, "inspector-label"), node("h2", active.label));
  if (active.description) panel.append(node("p", active.description));
  if (active.rule) panel.append(node("p", active.rule, "formula mono"));
  if (active.reference) {
    panel.append(node("h3", "Document reference"), node("p", active.reference.repository + " / " + active.reference.path, "mono"));
    if (active.reference.anchor) panel.append(node("p", "#" + active.reference.anchor, "mono"));
    if (sourceFile(active.reference)) panel.append(button("Open example document", () => showSource(active, active.reference.anchor).catch(showError), "text-button"));
    return;
  }

  const lookup = new Map(g.nodes.map(n => [n.id, n]));
  const relevant = state.trace?.edges || g.edges.filter(e => e.from === active.id);
  const evidence = relevant.filter(e => lookup.get(e.to)?.reference);
  evidence.sort((a, b) => Number(b.from === active.id) - Number(a.from === active.id) || a.id.localeCompare(b.id));
  panel.append(node("h3", "Evidence in reach"));
  if (!evidence.length) panel.append(node("p", "No source links in this trace."));
  const list = node("ul", undefined, "anchors");
  const seen = new Set();
  for (const edge of evidence) {
    const source = lookup.get(edge.to);
    const anchor = edge.anchor || source.reference.anchor;
    const key = edge.to + "#" + (anchor || "");
    if (seen.has(key)) continue;
    seen.add(key);
    const li = node("li");
    li.append(button(source.label, () => {
      if (sourceFile(source.reference)) showSource(source, anchor).catch(showError);
      else selectNode(source.id);
    }, "text-button"));
    li.append(node("small", anchor ? "#" + anchor : source.reference.path, "mono"));
    list.append(li);
  }
  panel.append(list);
  const frontier = state.trace?.frontier || [];
  if (frontier.length) {
    panel.append(node("h3", "Beyond this budget"));
    const pending = node("ul", undefined, "related");
    for (const id of frontier) {
      const item = lookup.get(id);
      if (!item) continue;
      const li = node("li");
      li.append(button(item.label, () => selectNode(item.id), "text-button"));
      pending.append(li);
    }
    panel.append(pending);
  }
}

async function traceNode() {
  const turn = ++state.tracing;
  if (!state.selected || state.review) return;
  const search = new URLSearchParams({node: state.selected, budget: $("budget").value, revision: state.graph.revision});
  const trace = await api(spacePath() + "/trace?" + search);
  if (turn !== state.tracing) return;
  state.trace = trace;
  draw();
}

function selectNode(id) {
  state.selected = id;
  state.trace = null;
  draw();
  traceNode().catch(showError);
}

function setView(view) {
  state.view = view;
  for (const tab of document.querySelectorAll("[data-view]")) tab.setAttribute("aria-pressed", String(tab.dataset.view === view));
  $("graph-viewport").scrollTo(0, 0);
  draw();
}

function openText(title, location, value) {
  $("text-title").textContent = title;
  $("text-location").textContent = location;
  $("text-body").textContent = value;
  $("text-dialog").showModal();
}

function renderQueue() {
  const list = $("queue");
  list.replaceChildren();
  if (!state.proposals.length) list.append(node("p", "No proposals in this workspace."));
  for (const p of state.proposals) {
    const item = node("div", undefined, "queue-item");
    const info = node("div");
    info.append(node("strong", p.title), node("p", p.status + " · based on r" + p.base_revision));
    item.append(info, button("Open preview", async () => {
      try {
        const result = await api(spacePath() + "/proposals/" + encodeURIComponent(p.id));
        state.review = result;
        state.trace = null;
        state.tracing++;
        if (!result.candidate.nodes.some(n => n.id === state.selected)) state.selected = result.candidate.nodes[0]?.id || "";
        $("review-dialog").close();
        draw();
      } catch (error) {
        list.prepend(node("p", error.message));
      }
    }));
    list.append(item);
  }
}

async function review(decision) {
  const p = state.review?.proposal;
  if (!p) return;
  if (decision === "approve" && !window.confirm("Publish this proposal as the next revision?")) return;
  $("approve").disabled = true;
  $("reject").disabled = true;
  try {
    const result = await api(spacePath() + "/proposals/" + encodeURIComponent(p.id) + "/review",
      {method: "POST", body: JSON.stringify({decision})});
    await loadSpace(state.space);
    notice(result.status === "approved" ? "Published revision r" + result.published_revision + "." : "Proposal rejected.");
  } catch (error) {
    draw();
    showError(error);
  }
}

async function progress(button, busy, work) {
  const label = button.textContent;
  button.disabled = true;
  button.textContent = busy;
  try {
    await work();
  } finally {
    button.disabled = false;
    button.textContent = label;
  }
}

for (const tab of document.querySelectorAll("[data-view]")) tab.addEventListener("click", () => setView(tab.dataset.view));
for (const b of document.querySelectorAll("[data-close]")) b.addEventListener("click", () => b.closest("dialog").close());
$("spaces").addEventListener("change", event => {
  notice("");
  loadSpace(event.target.value).catch(error => {
    event.target.value = state.space;
    showError(error);
  });
});
$("budget").addEventListener("change", () => traceNode().catch(showError));
$("proposals").addEventListener("click", async () => {
  if (!state.graph) return;
  try {
    state.proposals = await api(spacePath() + "/proposals");
    renderQueue();
    $("review-dialog").showModal();
    draw();
  } catch (error) {
    showError(error);
  }
});
$("exit-review").addEventListener("click", () => loadSpace(state.space).catch(showError));
$("changes").addEventListener("click", () => {
  const p = state.review.proposal;
  openText(p.title, "Patch against r" + p.base_revision, JSON.stringify(p.patch, null, 2));
});
$("approve").addEventListener("click", () => review("approve"));
$("reject").addEventListener("click", () => review("reject"));
$("export").addEventListener("click", async () => {
  try {
    const data = await api(spacePath() + "/graph?format=yaml&revision=" + state.graph.revision, {file: true});
    const url = URL.createObjectURL(data);
    const link = node("a");
    link.href = url;
    link.download = state.space + "-r" + state.graph.revision + ".yaml";
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch (error) {
    showError(error);
  }
});
$("draft").addEventListener("click", () => {
  state.draft = {space: state.space, base: state.graph.revision, id: crypto.randomUUID()};
  $("draft-form").reset();
  $("patch").value = '{\n  "upsert_nodes": [],\n  "upsert_edges": []\n}';
  $("draft-message").value = "";
  $("ai-fields").hidden = !state.assistant;
  $("draft-dialog").showModal();
});
$("generate").addEventListener("click", () => {
  if (!$("share").checked) {
    $("draft-message").value = "Confirm what will be sent to DeepSeek before generating a draft.";
    return;
  }
  const draft = state.draft;
  progress($("generate"), "Generating…", async () => {
    try {
      const idea = await api(spacePath(draft.space) + "/drafts", {method: "POST", body: JSON.stringify({
        instruction: $("instruction").value, share: true
      })});
      if (draft !== state.draft) return;
      draft.base = idea.base_revision;
      $("proposal-title").value = idea.title;
      $("patch").value = JSON.stringify(idea.patch, null, 2);
      $("draft-message").value = "Review the suggested patch, then submit it for approval.";
    } catch (error) {
      if (draft === state.draft) $("draft-message").value = error.message;
    }
  });
});
$("draft-form").addEventListener("submit", event => {
  event.preventDefault();
  const draft = state.draft;
  const submit = event.currentTarget.querySelector("[type=submit]");
  progress(submit, "Submitting…", async () => {
    try {
      const patch = JSON.parse($("patch").value);
      await api(spacePath(draft.space) + "/proposals", {method: "POST", body: JSON.stringify({
        request_id: draft.id, title: $("proposal-title").value, base_revision: draft.base, patch
      })});
      $("draft-dialog").close();
      await loadSpace(draft.space);
      notice("Proposal queued for review.");
    } catch (error) {
      $("draft-message").value = error.message;
    }
  });
});
$("unlock-form").addEventListener("submit", async event => {
  event.preventDefault();
  state.token = $("token").value;
  $("token").value = "";
  try {
    await connect();
    $("unlock-dialog").close();
  } catch (error) {
    state.token = "";
    $("unlock-message").value = error.message;
  }
});
$("unlock-dialog").addEventListener("cancel", event => event.preventDefault());

connect().catch(error => {
  if (error.status === 401) $("unlock-dialog").showModal();
  else showError(error);
});
