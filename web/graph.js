const ordering = ["domain", "metric", "concept", "source"];
const names = {domain: "Domain", metric: "Metrics", concept: "Business drivers", source: "Source documents"};

function groupNodes(nodes) {
  return Object.fromEntries(ordering.map(kind => [kind, nodes.filter(n => n.kind === kind)]));
}

function flowLayout(groups, width, minHeight) {
  const columns = ["metric", "concept", "source"];
  const height = Math.max(minHeight, 340, 85 + Math.max(...columns.map(k => groups[k].length)) * 75);
  const guides = [];
  const points = new Map();
  const card = Math.max(130, Math.min(172, width / 3 - 38));
  columns.forEach((kind, col) => {
    const x = width * (col + .5) / 3;
    const nodes = groups[kind];
    guides.push({name: names[kind], x, className: "column"});
    nodes.forEach((node, i) => {
      const compact = Math.min(height - 145, (nodes.length - 1) * 112);
      const offset = nodes.length > 1 ? (i / (nodes.length - 1) - .5) * compact : 0;
      points.set(node.id, {node, x, y: (height + 40) / 2 + offset, w: card});
    });
  });
  return {points, guides, height, bands: []};
}

function layerLayout(groups, width, minHeight) {
  const columns = Math.max(2, Math.floor((width - 114) / 164));
  const card = Math.max(129, Math.min(156, (width - 130) / columns - 15));
  let y = 0;
  const guides = [];
  const bands = [];
  const points = new Map();
  const visible = ordering.filter(k => groups[k].length);
  const minimumLane = Math.max(82, (minHeight - 24) / Math.max(visible.length, 1));
  for (const kind of visible) {
    const nodes = groups[kind];
    const rows = Math.ceil(nodes.length / columns);
    const height = Math.max(minimumLane, rows * 66 + 26);
    const title = {domain:"Domains", metric:"Metrics", concept:"Concepts", source:"Sources"}[kind];
    guides.push({name: title, y: y + height / 2, className: "row"});
    bands.push({top: y + 8, bottom: y + height - 8});
    nodes.forEach((node, i) => {
      const row = Math.floor(i / columns), index = i % columns;
      const count = Math.min(columns, nodes.length - row * columns);
      const dx = Math.min(card + 32, (width - 125) / count);
      const x = (width + 105) / 2 + (index - (count - 1) / 2) * dx + (row % 2 ? 6 : -6);
      points.set(node.id, {node, x, y: y + height / 2 + (row - (rows - 1) / 2) * 62, w: card});
    });
    y += height;
  }
  return {points, guides, height: Math.max(minHeight, y), bands};
}

function arrow(ctx, x, y, dx, dy) {
  const angle = Math.atan2(dy, dx);
  ctx.moveTo(x - 7 * Math.cos(angle - .55), y - 7 * Math.sin(angle - .55));
  ctx.lineTo(x, y);
  ctx.lineTo(x - 7 * Math.cos(angle + .55), y - 7 * Math.sin(angle + .55));
}

function strokeEdge(ctx, edge, from, to, active, pending, layered, width) {
  ctx.strokeStyle = pending ? "#e5b960" : active ? "#72bcaa" : "#4a625e";
  if (active && edge.kind === "affected_by") ctx.strokeStyle = "#d1aa6c";
  ctx.lineWidth = pending || active ? 1.55 : 1;
  ctx.setLineDash(pending ? [5, 4] : edge.kind === "affected_by" ? [4, 4] : []);
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  ctx.beginPath();
  if (Math.abs(dx) < 12 || (layered && Math.abs(dy) >= 55)) {
    const sign = Math.sign(dy) || 1;
    const start = from.y + 21 * sign, end = to.y - 23 * sign;
    const bend = Math.max(12, Math.abs(end - start) * .5);
    ctx.moveTo(from.x, start);
    ctx.bezierCurveTo(from.x, start + bend * sign, to.x, end - bend * sign, to.x, end);
    arrow(ctx, to.x, end, 0, sign);
  } else {
    const sign = Math.sign(dx);
    const start = from.x + from.w / 2 * sign, end = to.x - (to.w / 2 + 3) * sign;
    const mid = Math.max(18, Math.abs(end - start) * .55);
    const long = !layered && Math.abs(dx) > width / 2;
    const bow = long ? (dy <= 0 ? -104 : 104) : 0;
    ctx.moveTo(start, from.y);
    ctx.bezierCurveTo(start + mid * sign, from.y + bow, end - mid * sign, to.y + bow, end, to.y);
    arrow(ctx, end, to.y, mid * sign, -bow);
  }
  ctx.stroke();
  ctx.setLineDash([]);
}

export function attachDiagram(host, choose) {
  const viewport = host.parentElement;
  const canvas = host.querySelector("canvas");
  const guides = host.querySelector(".graph-guides");
  const nodes = host.querySelector(".graph-nodes");
  let model;
  let frame = 0;

  function queue() {
    if (frame) cancelAnimationFrame(frame);
    frame = requestAnimationFrame(render);
  }

  function render() {
    frame = 0;
    if (!model) return;
    const focused = document.activeElement?.dataset.graphId;
    const width = host.clientWidth;
    const groups = groupNodes(model.snapshot.nodes);
    const layout = model.view === "layers" ? layerLayout(groups, width, viewport.clientHeight) : flowLayout(groups, width, viewport.clientHeight);
    host.style.height = layout.height + "px";
    const dpr = Math.min(window.devicePixelRatio || 1, 8192 / Math.max(width, layout.height),
      Math.sqrt(12_000_000 / (width * layout.height)));
    canvas.width = Math.round(width * dpr);
    canvas.height = Math.round(layout.height * dpr);
    const ctx = canvas.getContext("2d");
    ctx.scale(dpr, dpr);
    for (const band of layout.bands) {
      ctx.fillStyle = "#29483d45";
      ctx.fillRect(0, band.top, width, band.bottom - band.top);
      ctx.strokeStyle = "#527166";
      ctx.beginPath();
      ctx.moveTo(0, band.bottom);
      ctx.lineTo(width, band.bottom);
      ctx.stroke();
    }
    const changed = model.markedEdges;
    const traced = new Set(model.trace?.edges.map(edge => edge.id));
    const keep = new Set(model.trace?.steps.map(step => step.node.id));
    const draw = (edge) => {
      const from = layout.points.get(edge.from), to = layout.points.get(edge.to);
      if (from && to) strokeEdge(ctx, edge, from, to, traced.has(edge.id), changed.has(edge.id), model.view === "layers", width);
    };
    model.snapshot.edges.filter(e => !traced.has(e.id) && !changed.has(e.id)).forEach(draw);
    model.snapshot.edges.filter(e => traced.has(e.id) || changed.has(e.id)).forEach(draw);

    guides.replaceChildren(...layout.guides.map(({name, x, y, className}) => {
      const label = document.createElement("span");
      label.className = "graph-guide " + className;
      label.textContent = name;
      if (x !== undefined) label.style.left = x + "px";
      if (y !== undefined) label.style.top = y + "px";
      return label;
    }));
    nodes.replaceChildren(...Array.from(layout.points.values(), ({node, x, y, w}) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "graph-node " + node.kind;
      button.dataset.graphId = node.id;
      button.textContent = node.label;
      button.setAttribute("aria-current", String(node.id === model.selected));
      if (model.markedNodes.has(node.id)) button.classList.add("pending");
      if (keep.size && !keep.has(node.id)) button.classList.add("dim");
      button.style.setProperty("--node-width", w + "px");
      button.style.left = x - w / 2 + "px";
      button.style.top = y + "px";
      button.style.transform = "translateY(-50%)";
      button.addEventListener("click", () => choose(node.id));
      return button;
    }));
    if (focused) Array.from(nodes.children).find(b => b.dataset.graphId === focused)?.focus({preventScroll: true});
  }

  new ResizeObserver(queue).observe(viewport);
  return next => {
    model = next;
    queue();
  };
}
