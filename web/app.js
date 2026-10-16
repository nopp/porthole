const $ = (id) => document.getElementById(id);

const state = {
  pods: [],
  selected: null,
  lang: "en",
  theme: "dark",
  checkPayload: null,
};

const I18N = {
  "pt-BR": {
    tagline: "Entenda os limites de rede do seu cluster.",
    loading: "carregando…",
    language: "Idioma",
    theme: "Tema",
    themeDark: "Escuro",
    themeLight: "Claro",
    refresh: "Atualizar",
    namespace: "Namespace",
    filter: "Filtrar",
    filterPlaceholder: "nome ou label",
    allNamespaces: "todos",
    empty: "Escolha um pod para ver quais NetworkPolicies o isolam e para simular um fluxo.",
    colPolicy: "Policy",
    colSelects: "Seleciona",
    colIsolates: "Isola",
    colRules: "Regras",
    colNotes: "Notas",
    simTitle: "Pod A consegue falar com pod B?",
    from: "De",
    to: "Para",
    orIp: "ou IP",
    protocol: "Protocolo",
    port: "Porta",
    simulate: "Simular",
    graphTitle: "Grafo do pod",
    graphAria: "Grafo de reachability do pod em foco",
    allowedEdge: "permitido",
    deniedEdge: "negado",
    noPods: "Nenhum pod.",
    noIp: "sem IP",
    yes: "sim",
    no: "não",
    noPolicies: "Nenhuma NetworkPolicy neste namespace.",
    ingressClosed: "Ingress isolado",
    ingressOpen: "Ingress aberto",
    egressClosed: "Egress isolado",
    egressOpen: "Egress aberto",
    allowed: "Permitido",
    denied: "Negado",
    isolated: "isolado",
    notIsolated: "não isolado",
    error: "Erro",
    pickTarget: "Escolha um pod de destino ou informe um IP.",
    pods: "pods",
    policies: "policies",
    errorWord: "erro",
  },
  en: {
    tagline: "Understand your cluster's network boundaries.",
    loading: "loading…",
    language: "Language",
    theme: "Theme",
    themeDark: "Dark",
    themeLight: "Light",
    refresh: "Refresh",
    namespace: "Namespace",
    filter: "Filter",
    filterPlaceholder: "name or label",
    allNamespaces: "all",
    empty: "Pick a pod to see which NetworkPolicies isolate it and to simulate a flow.",
    colPolicy: "Policy",
    colSelects: "Selects",
    colIsolates: "Isolates",
    colRules: "Rules",
    colNotes: "Notes",
    simTitle: "Can pod A reach pod B?",
    from: "From",
    to: "To",
    orIp: "or IP",
    protocol: "Protocol",
    port: "Port",
    simulate: "Simulate",
    graphTitle: "Pod graph",
    graphAria: "Reachability graph for the focused pod",
    allowedEdge: "allowed",
    deniedEdge: "denied",
    noPods: "No pods.",
    noIp: "no IP",
    yes: "yes",
    no: "no",
    noPolicies: "No NetworkPolicy in this namespace.",
    ingressClosed: "Ingress isolated",
    ingressOpen: "Ingress open",
    egressClosed: "Egress isolated",
    egressOpen: "Egress open",
    allowed: "Allowed",
    denied: "Denied",
    isolated: "isolated",
    notIsolated: "not isolated",
    error: "Error",
    pickTarget: "Choose a destination pod or enter an IP.",
    pods: "pods",
    policies: "policies",
    errorWord: "error",
  },
};

function t(key) {
  const pack = I18N[state.lang] || I18N["pt-BR"];
  return pack[key] || I18N["pt-BR"][key] || key;
}

function applyI18n() {
  document.documentElement.lang = state.lang === "en" ? "en" : "pt-BR";
  document.querySelectorAll("[data-i18n]").forEach((el) => {
    el.textContent = t(el.dataset.i18n);
  });
  document.querySelectorAll("[data-i18n-placeholder]").forEach((el) => {
    el.placeholder = t(el.dataset.i18nPlaceholder);
  });
  document.querySelectorAll("[data-i18n-aria]").forEach((el) => {
    el.setAttribute("aria-label", t(el.dataset.i18nAria));
  });
}

function applyTheme() {
  document.documentElement.dataset.theme = state.theme;
}

function esc(value) {
  return String(value ?? "").replace(/[&<>"']/g, (ch) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[ch]));
}

function withLang(path) {
  const lang = state.lang === "en" ? "en" : "pt-BR";
  const join = path.includes("?") ? "&" : "?";
  return path + join + "lang=" + encodeURIComponent(lang);
}

async function api(path, opts) {
  const res = await fetch(withLang(path), opts);
  const text = await res.text();
  let body = {};
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = { error: text };
    }
  }
  if (!res.ok) {
    throw new Error(body.error || res.statusText);
  }
  return body;
}

function keyOf(pod) {
  return pod.namespace + "/" + pod.name;
}

function parseKey(value) {
  const i = value.indexOf("/");
  if (i <= 0) return null;
  return { namespace: value.slice(0, i), name: value.slice(i + 1) };
}

function labelsOf(labels) {
  return Object.entries(labels || {})
    .map(([k, v]) => k + "=" + v)
    .join(", ");
}

async function loadAll() {
  const [meta, namespaces, pods] = await Promise.all([
    api("/api/meta"),
    api("/api/namespaces"),
    api("/api/pods"),
  ]);
  state.pods = pods;
  const when = meta.loadedAt ? new Date(meta.loadedAt).toLocaleString(state.lang === "en" ? "en" : "pt-BR") : "—";
  const err = meta.refreshError ? " · " + t("errorWord") + ": " + meta.refreshError : "";
  $("meta").textContent = `${meta.source || "snapshot"} · ${meta.namespaces || 0} ns · ${meta.pods || 0} ${t("pods")} · ${meta.policies || 0} ${t("policies")} · ${when}${err}`;

  const ns = $("ns");
  const current = ns.value;
  ns.innerHTML = `<option value="">${esc(t("allNamespaces"))}</option>` + namespaces.map((n) =>
    `<option value="${esc(n.name)}">${esc(n.name)}</option>`).join("");
  if ([...ns.options].some((o) => o.value === current)) ns.value = current;
  renderPods();
  fillPodSelects();
}

function visiblePods() {
  const ns = $("ns").value;
  const q = $("q").value.trim().toLowerCase();
  return state.pods.filter((pod) => {
    if (ns && pod.namespace !== ns) return false;
    if (!q) return true;
    const blob = (keyOf(pod) + " " + labelsOf(pod.labels) + " " + (pod.ips || []).join(" ")).toLowerCase();
    return blob.includes(q);
  });
}

function renderPods() {
  const list = $("pods");
  const pods = visiblePods();
  list.innerHTML = pods.map((pod) => {
    const active = state.selected === keyOf(pod) ? " active" : "";
    const inn = pod.ingressIsolated ? "on in" : "";
    const eg = pod.egressIsolated ? "on eg" : "";
    const ip = (pod.ips || [])[0] || t("noIp");
    return `<button type="button" class="pod${active}" data-key="${esc(keyOf(pod))}">
      <strong>${esc(pod.name)}</strong>
      <span class="badges"><span class="badge ${inn}">in</span><span class="badge ${eg}">eg</span></span>
      <span class="sub">${esc(pod.namespace)} · ${esc(ip)}</span>
    </button>`;
  }).join("") || `<p class="meta">${esc(t("noPods"))}</p>`;
}

function fillPodSelects() {
  const options = state.pods.map((pod) =>
    `<option value="${esc(keyOf(pod))}">${esc(keyOf(pod))}</option>`).join("");
  const from = $("from");
  const to = $("to");
  const fromVal = from.value;
  const toVal = to.value;
  from.innerHTML = options;
  to.innerHTML = '<option value="">—</option>' + options;
  if (fromVal) from.value = fromVal;
  if (toVal) to.value = toVal;
}

async function selectPod(key) {
  state.selected = key;
  renderPods();
  const ref = parseKey(key);
  if (!ref) return;
  $("empty").classList.add("hidden");
  $("detail").classList.remove("hidden");
  $("from").value = key;
  const explain = await api(`/api/pods/${encodeURIComponent(ref.namespace)}/${encodeURIComponent(ref.name)}`);
  renderExplain(explain);
  if (explain.ports && explain.ports.length === 1) {
    $("port").value = explain.ports[0].port;
    $("protocol").value = explain.ports[0].protocol || "TCP";
  }
  await loadGraph();
}

function renderExplain(ex) {
  $("pod-title").textContent = ex.namespace + "/" + ex.name;
  const bits = [];
  if (ex.phase) bits.push(ex.phase);
  if (ex.ips && ex.ips.length) bits.push(ex.ips.join(", "));
  if (ex.hostNetwork) bits.push("hostNetwork");
  const labs = labelsOf(ex.labels);
  if (labs) bits.push(labs);
  if (ex.ports && ex.ports.length) {
    bits.push(ex.ports.map((p) => (p.name ? p.name + ":" : "") + p.port + "/" + p.protocol).join(", "));
  }
  $("pod-meta").textContent = bits.join(" · ");
  setChip($("iso-in"), ex.ingressIsolated, t("ingressClosed"), t("ingressOpen"));
  setChip($("iso-eg"), ex.egressIsolated, t("egressClosed"), t("egressOpen"));
  $("pod-notes").innerHTML = (ex.notes || []).map((n) => `<li>${esc(n)}</li>`).join("");
  $("policy-body").innerHTML = (ex.policies || []).map((pol) => {
    const isola = [
      pol.isolatesIngress ? "Ingress" : "",
      pol.isolatesEgress ? "Egress" : "",
    ].filter(Boolean).join(", ") || "—";
    const rules = (pol.rules || []).map((r) => `<span class="rule">${esc(r)}</span>`).join("");
    const notes = (pol.notes || []).map((n) => `<span class="rule warn">${esc(n)}</span>`).join("");
    return `<tr>
      <td class="mono">${esc(pol.namespace)}/${esc(pol.name)}</td>
      <td class="${pol.selects ? "yes" : "no"}">${pol.selects ? esc(t("yes")) : esc(t("no"))}</td>
      <td>${esc(isola)}</td>
      <td>${rules}</td>
      <td>${notes}</td>
    </tr>`;
  }).join("") || `<tr><td colspan="5">${esc(t("noPolicies"))}</td></tr>`;
}

function setChip(el, closed, closedText, openText) {
  el.textContent = closed ? closedText : openText;
  el.classList.toggle("closed", closed);
}

async function loadGraph() {
  if (!state.selected) return;
  const ref = parseKey(state.selected);
  const params = new URLSearchParams({
    namespace: ref.namespace,
    pod: ref.name,
    protocol: $("protocol").value,
    port: $("port").value || "80",
  });
  const graph = await api("/api/graph?" + params.toString());
  $("graph-notes").textContent = [
    graph.protocol + "/" + graph.port,
    ...(graph.notes || []),
  ].join(" · ");
  drawGraph(graph);
}

function drawGraph(graph) {
  const svg = $("graph");
  svg.replaceChildren();
  const ns = "http://www.w3.org/2000/svg";
  const defs = document.createElementNS(ns, "defs");
  defs.append(marker(ns, "arrow-ok", "#2faf84"), marker(ns, "arrow-no", "#e15d6c"));
  svg.appendChild(defs);

  const nodes = graph.nodes || [];
  const focus = nodes.find((n) => n.focus) || nodes[0];
  const others = nodes.filter((n) => n !== focus);
  const pos = new Map();
  pos.set(focus.id, { x: 480, y: 280 });
  others.forEach((node, i) => {
    const angle = -Math.PI / 2 + (2 * Math.PI * i) / Math.max(others.length, 1);
    const radius = node.kind === "cidr" ? 230 : 200;
    pos.set(node.id, {
      x: 480 + radius * Math.cos(angle),
      y: 280 + radius * Math.sin(angle),
    });
  });

  const pair = new Set((graph.edges || []).map((e) => e.from + ">" + e.to));
  for (const edge of graph.edges || []) {
    const a = pos.get(edge.from);
    const b = pos.get(edge.to);
    if (!a || !b) continue;
    const back = pair.has(edge.to + ">" + edge.from);
    const line = offsetLine(a, b, back ? 7 : 0);
    const el = document.createElementNS(ns, "line");
    el.setAttribute("x1", line.x1);
    el.setAttribute("y1", line.y1);
    el.setAttribute("x2", line.x2);
    el.setAttribute("y2", line.y2);
    el.setAttribute("stroke", edge.allowed ? "#2faf84" : "#e15d6c");
    el.setAttribute("stroke-width", edge.allowed ? "1.8" : "1.3");
    if (!edge.allowed) el.setAttribute("stroke-dasharray", "5 4");
    el.setAttribute("marker-end", edge.allowed ? "url(#arrow-ok)" : "url(#arrow-no)");
    const title = document.createElementNS(ns, "title");
    title.textContent = edge.reason || "";
    el.appendChild(title);
    svg.appendChild(el);
  }

  for (const node of nodes) {
    const p = pos.get(node.id);
    const g = document.createElementNS(ns, "g");
    g.setAttribute("transform", `translate(${p.x}, ${p.y})`);
    if (node.kind === "pod") {
      g.style.cursor = "pointer";
      g.addEventListener("click", () => selectPod(node.namespace + "/" + node.name).catch(showError));
    }
    const box = document.createElementNS(ns, "rect");
    const w = node.kind === "cidr" ? 150 : 124;
    box.setAttribute("x", -w / 2);
    box.setAttribute("y", -18);
    box.setAttribute("width", w);
    box.setAttribute("height", 36);
    box.setAttribute("rx", 7);
    box.setAttribute("class", "node" + (node.focus ? " focus" : "") + (node.kind === "cidr" ? " cidr" : ""));
    g.appendChild(box);
    const label = document.createElementNS(ns, "text");
    label.setAttribute("class", "label");
    label.setAttribute("text-anchor", "middle");
    label.setAttribute("y", node.namespace && !node.focus ? -1 : 4);
    label.setAttribute("font-size", "12");
    label.setAttribute("font-family", "ui-monospace, Menlo, Consolas, monospace");
    label.textContent = node.name;
    g.appendChild(label);
    if (node.namespace && !node.focus) {
      const sub = document.createElementNS(ns, "text");
      sub.setAttribute("class", "sub");
      sub.setAttribute("text-anchor", "middle");
      sub.setAttribute("y", 13);
      sub.setAttribute("font-size", "10");
      sub.textContent = node.namespace;
      g.appendChild(sub);
    }
    svg.appendChild(g);
  }
}

function marker(ns, id, color) {
  const mark = document.createElementNS(ns, "marker");
  mark.setAttribute("id", id);
  mark.setAttribute("viewBox", "0 0 10 10");
  mark.setAttribute("refX", "9");
  mark.setAttribute("refY", "5");
  mark.setAttribute("markerWidth", "7");
  mark.setAttribute("markerHeight", "7");
  mark.setAttribute("orient", "auto-start-reverse");
  const path = document.createElementNS(ns, "path");
  path.setAttribute("d", "M 0 0 L 10 5 L 0 10 z");
  path.setAttribute("fill", color);
  mark.appendChild(path);
  return mark;
}

function offsetLine(a, b, amount) {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const len = Math.hypot(dx, dy) || 1;
  const ox = (-dy / len) * amount;
  const oy = (dx / len) * amount;
  const trim = 78;
  return {
    x1: a.x + ox + (dx / len) * trim,
    y1: a.y + oy + (dy / len) * trim,
    x2: b.x + ox - (dx / len) * (trim - 8),
    y2: b.y + oy - (dy / len) * (trim - 8),
  };
}

function showError(err) {
  const box = $("verdict");
  box.classList.remove("hidden", "allow");
  box.classList.add("deny");
  box.innerHTML = `<h3>${esc(t("error"))}</h3><p>${esc(err.message || err)}</p>`;
}

async function runCheck(event) {
  event.preventDefault();
  const payload = {
    protocol: $("protocol").value,
    port: Number($("port").value),
    from: parseKey($("from").value),
  };
  const ip = $("to-ip").value.trim();
  if (ip) {
    payload.toIP = ip;
  } else if ($("to").value) {
    payload.to = parseKey($("to").value);
  } else {
    showError(new Error(t("pickTarget")));
    return;
  }
  state.checkPayload = payload;
  const res = await api("/api/check", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  renderVerdict(res);
  await loadGraph();
}

function renderVerdict(res) {
  const box = $("verdict");
  box.classList.remove("hidden", "allow", "deny");
  box.classList.add(res.allowed ? "allow" : "deny");
  const sides = [res.egress, res.ingress].filter(Boolean).map(renderSide).join("");
  box.innerHTML = `<h3>${res.allowed ? esc(t("allowed")) : esc(t("denied"))}</h3>
    <p>${esc(res.summary)}</p>
    ${sides ? `<div class="sides">${sides}</div>` : ""}`;
}

function renderSide(side) {
  const rows = (side.policies || []).map((p) => {
    const cls = p.matched ? "hit" : "";
    return `<li class="${cls}">${esc(p.namespace)}/${esc(p.name)}: ${esc(p.detail)}</li>`;
  }).join("");
  return `<div class="side">
    <p><strong>${esc(side.direction)}</strong> · ${side.isolated ? esc(t("isolated")) : esc(t("notIsolated"))} · ${side.allowed ? esc(t("allowedEdge")) : esc(t("deniedEdge"))}</p>
    <p>${esc(side.reason)}</p>
    <ul class="trace">${rows}</ul>
  </div>`;
}

$("pods").addEventListener("click", (event) => {
  const btn = event.target.closest(".pod");
  if (!btn) return;
  selectPod(btn.dataset.key).catch(showError);
});

$("ns").addEventListener("change", renderPods);
$("q").addEventListener("input", renderPods);
$("refresh").addEventListener("click", async () => {
  $("refresh").disabled = true;
  try {
    await api("/api/refresh", { method: "POST" });
    await loadAll();
    if (state.selected) await selectPod(state.selected);
  } catch (err) {
    showError(err);
  } finally {
    $("refresh").disabled = false;
  }
});
$("sim").addEventListener("submit", (event) => {
  runCheck(event).catch(showError);
});
$("protocol").addEventListener("change", () => loadGraph().catch(showError));
$("port").addEventListener("change", () => loadGraph().catch(showError));

$("lang").addEventListener("change", async () => {
  state.lang = $("lang").value === "en" ? "en" : "pt-BR";
  localStorage.setItem("porthole.lang", state.lang);
  applyI18n();
  try {
    await loadAll();
    if (state.selected) await selectPod(state.selected);
    if (state.checkPayload) {
      const res = await api("/api/check", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(state.checkPayload),
      });
      renderVerdict(res);
    }
  } catch (err) {
    showError(err);
  }
});

$("theme").addEventListener("change", () => {
  state.theme = $("theme").value === "light" ? "light" : "dark";
  localStorage.setItem("porthole.theme", state.theme);
  applyTheme();
});

state.lang = localStorage.getItem("porthole.lang") === "pt-BR" ? "pt-BR" : "en";
state.theme = localStorage.getItem("porthole.theme") === "light" ? "light" : "dark";
$("lang").value = state.lang;
$("theme").value = state.theme;
applyTheme();
applyI18n();
loadAll().catch(showError);
