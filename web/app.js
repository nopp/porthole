const $ = (id) => document.getElementById(id);

const state = {
  pods: [],
  selected: null,
  booted: false,
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
    pod: "Pod",
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
    pod: "Pod",
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

  const names = namespaces.map((n) => n.name);
  const defaultNS = state.pods[0]?.namespace || "";
  const fromNS = $("from-ns").value || defaultNS;
  const toNS = $("to-ns").value || fromNS;
  const fromPod = state.booted ? $("from").value : null;
  const toPod = state.booted ? $("to").value : null;
  fillNamespaceSelect($("from-ns"), names, fromNS);
  fillNamespaceSelect($("to-ns"), names, toNS);
  fillPodSelect($("from"), $("from-ns").value, fromPod, false);
  fillPodSelect($("to"), $("to-ns").value, toPod, true);
  if (!state.booted) preferAnotherTarget();
  state.booted = true;

  const hasPods = state.pods.length > 0;
  $("detail").classList.toggle("hidden", !hasPods);
  $("none").classList.toggle("hidden", hasPods);
  if (hasPods) await showFrom();
}

function fillNamespaceSelect(select, names, preferred) {
  select.innerHTML = names.map((name) => `<option value="${esc(name)}">${esc(name)}</option>`).join("");
  if (preferred && [...select.options].some((o) => o.value === preferred)) select.value = preferred;
}

function fillPodSelect(select, namespace, preferred, allowEmpty) {
  const pods = state.pods.filter((pod) => pod.namespace === namespace);
  const opts = pods.map((pod) => `<option value="${esc(keyOf(pod))}">${esc(pod.name)}</option>`).join("");
  select.innerHTML = (allowEmpty ? `<option value="">—</option>` : "") + opts;
  if (!opts && !allowEmpty) {
    select.innerHTML = `<option value="">${esc(t("noPods"))}</option>`;
    return;
  }
  if (preferred != null && [...select.options].some((o) => o.value === preferred)) {
    select.value = preferred;
    return;
  }
  const first = [...select.options].find((o) => o.value);
  select.value = first ? first.value : "";
}

function preferAnotherTarget() {
  const other = [...$("to").options].find((o) => o.value && o.value !== $("from").value);
  if (other) $("to").value = other.value;
}

function hasTarget() {
  return Boolean($("to-ip").value.trim() || $("to").value);
}

async function chooseFrom(namespace, name) {
  $("from-ns").value = namespace;
  fillPodSelect($("from"), namespace, namespace + "/" + name, false);
  await showFrom();
}

async function showFrom() {
  const key = $("from").value;
  const ref = parseKey(key);
  $("explain").classList.toggle("hidden", !ref);
  $("graph-panel").classList.toggle("hidden", !ref);
  if (!ref) {
    state.selected = null;
    $("verdict").classList.add("hidden");
    return;
  }
  const changed = state.selected !== key;
  state.selected = key;
  const explain = await api(`/api/pods/${encodeURIComponent(ref.namespace)}/${encodeURIComponent(ref.name)}`);
  renderExplain(explain);
  if (changed && explain.ports && explain.ports.length === 1) {
    $("port").value = explain.ports[0].port;
    $("protocol").value = explain.ports[0].protocol || "TCP";
  }
  await loadGraph();
  if (hasTarget()) await runCheck();
  else $("verdict").classList.add("hidden");
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
  const ip = $("to-ip").value.trim();
  const to = parseKey($("to").value);
  if (ip) {
    params.set("toIP", ip);
  } else if (to) {
    params.set("toNamespace", to.namespace);
    params.set("toPod", to.name);
  }
  const graph = await api("/api/graph?" + params.toString());
  $("graph-notes").textContent = [
    graph.protocol + "/" + graph.port,
    ...(graph.notes || []),
  ].join(" · ");
  drawGraph(graph);
}

// Replica suffixes sit at the end. Keep that tail when the name is wider than the box.
function fitGraphLabel(el, text, maxPx) {
  const full = text || "";
  el.textContent = full;
  if (!full) return;

  const px = (s) => {
    el.textContent = s;
    const measured = el.getComputedTextLength();
    return measured > 0 ? measured : s.length * 7.8;
  };
  if (px(full) <= maxPx) return;

  const ell = "…";
  const tailLen = Math.min(7, full.length - 2);
  const tail = tailLen > 0 ? full.slice(-tailLen) : "";
  for (let n = full.length - tailLen - 1; n > 0; n--) {
    const candidate = full.slice(0, n) + ell + tail;
    if (px(candidate) <= maxPx) return;
  }

  let fitted = ell;
  for (let n = 1; n < full.length; n++) {
    const candidate = full.slice(0, n) + ell;
    if (px(candidate) > maxPx) break;
    fitted = candidate;
  }
  el.textContent = fitted;
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
      g.addEventListener("click", () => chooseFrom(node.namespace, node.name).catch(showError));
    }
    const box = document.createElementNS(ns, "rect");
    const w = node.kind === "cidr" ? 150 : 124;
    const showPort = !node.focus && graph.port;
    const h = showPort ? 50 : 36;
    box.setAttribute("x", -w / 2);
    box.setAttribute("y", showPort ? -25 : -18);
    box.setAttribute("width", w);
    box.setAttribute("height", h);
    box.setAttribute("rx", 7);
    box.setAttribute("class", "node" + (node.focus ? " focus" : "") + (node.kind === "cidr" ? " cidr" : ""));
    g.appendChild(box);
    const tip = document.createElementNS(ns, "title");
    tip.textContent = node.namespace ? node.namespace + "/" + node.name : node.name;
    g.appendChild(tip);
    svg.appendChild(g);
    const label = document.createElementNS(ns, "text");
    label.setAttribute("class", "label");
    label.setAttribute("text-anchor", "middle");
    label.setAttribute("y", node.namespace ? (showPort ? -10 : -1) : (showPort ? -4 : 4));
    label.setAttribute("font-size", "12");
    label.setAttribute("font-family", "ui-monospace, Menlo, Consolas, monospace");
    g.appendChild(label);
    fitGraphLabel(label, node.name, w - 18);
    if (node.namespace) {
      const sub = document.createElementNS(ns, "text");
      sub.setAttribute("class", "sub");
      sub.setAttribute("text-anchor", "middle");
      sub.setAttribute("y", showPort ? 4 : 13);
      sub.setAttribute("font-size", "10");
      g.appendChild(sub);
      fitGraphLabel(sub, node.namespace, w - 18);
    }
    if (showPort) {
      const port = document.createElementNS(ns, "text");
      port.setAttribute("class", "sub");
      port.setAttribute("text-anchor", "middle");
      port.setAttribute("y", 18);
      port.setAttribute("font-size", "10");
      port.setAttribute("font-family", "ui-monospace, Menlo, Consolas, monospace");
      g.appendChild(port);
      fitGraphLabel(port, (graph.protocol || "TCP") + "/" + graph.port, w - 18);
    }
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
  if (event) event.preventDefault();
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

$("from-ns").addEventListener("change", () => {
  fillPodSelect($("from"), $("from-ns").value, null, false);
  showFrom().catch(showError);
});
$("from").addEventListener("change", () => showFrom().catch(showError));
$("to-ns").addEventListener("change", () => {
  fillPodSelect($("to"), $("to-ns").value, null, true);
  preferAnotherTarget();
  refreshFlow().catch(showError);
});
function refreshFlow() {
  const jobs = [loadGraph()];
  if (hasTarget()) jobs.push(runCheck());
  else $("verdict").classList.add("hidden");
  return Promise.all(jobs);
}
$("to").addEventListener("change", () => refreshFlow().catch(showError));
$("to-ip").addEventListener("change", () => refreshFlow().catch(showError));
$("refresh").addEventListener("click", async () => {
  $("refresh").disabled = true;
  try {
    await api("/api/refresh", { method: "POST" });
    await loadAll();
  } catch (err) {
    showError(err);
  } finally {
    $("refresh").disabled = false;
  }
});
$("sim").addEventListener("submit", (event) => {
  event.preventDefault();
  refreshFlow().catch(showError);
});
$("protocol").addEventListener("change", () => refreshFlow().catch(showError));
$("port").addEventListener("change", () => refreshFlow().catch(showError));

$("lang").addEventListener("change", async () => {
  state.lang = $("lang").value === "en" ? "en" : "pt-BR";
  localStorage.setItem("porthole.lang", state.lang);
  applyI18n();
  try {
    await loadAll();
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
