"use strict";

const $ = (sel) => document.querySelector(sel);

// The parameter form's field metadata (path, label and tip per field) is
// served by GET /api/fields, so it lives in one place on the server, with a
// test that every parameter has an entry.
let fieldGroups = [];

const fmtInt = new Intl.NumberFormat("en-US");
const fmtMoney = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 });

let preset = null; // defaults for the selected LoB, as served by the API
let presetId = null; // the preset those defaults came from, which decides how a run is scored

async function fetchJSON(url, options) {
  const res = await fetch(url, options);
  let body = null;
  try {
    body = await res.json();
  } catch {
    // Non-JSON response (e.g. a plain-text 404); fall back to statusText.
  }
  if (!res.ok) throw new Error((body && body.error) || res.statusText);
  return body;
}

const getPath = (obj, path) => path.reduce((o, k) => o[k], obj);

function showError(msg) {
  const banner = $("#error-banner");
  banner.textContent = msg;
  banner.hidden = false;
}
const clearError = () => { $("#error-banner").hidden = true; };

async function loadLOBs() {
  const lobs = await fetchJSON("/api/lobs");
  const select = $("#lob-select");
  select.replaceChildren(...lobs.map((l) => {
    const option = document.createElement("option");
    option.value = l.id;
    option.textContent = l.name;
    return option;
  }));
  select.addEventListener("change", () => loadPreset(select.value).catch((e) => showError(e.message)));
  await loadPreset(select.value);
}

async function loadFields() {
  fieldGroups = await fetchJSON("/api/fields");
}

async function loadPreset(id) {
  preset = await fetchJSON(`/api/lobs/${encodeURIComponent(id)}/preset`);
  presetId = id;
  buildParamsForm();
}

// The server caps run size; mirroring the caps as input bounds turns a
// rejected run into a form validation message, and keeps the numbers in one
// place rather than restating them here.
async function loadLimits() {
  const limits = await fetchJSON("/api/limits");
  $("#years").max = limits.max_years;
  $("#initial-book-size").max = limits.max_initial_book_size;
}

function numberInput(value, path) {
  const input = document.createElement("input");
  input.type = "number";
  input.step = "any";
  input.required = true;
  input.value = value;
  if (path) input.dataset.path = JSON.stringify(path);
  return input;
}

// A group with a sections path is shown once per section in the preset, its
// field paths relative to the section; a field with a kind applies only to
// sections whose severity is of that kind.
function buildParamsForm() {
  const root = $("#params-form");
  root.replaceChildren();
  for (const group of fieldGroups) {
    if (group.sections) {
      getPath(preset, group.sections).forEach((section, i) => {
        const fields = group.fields.filter((f) => !f.kind || f.kind === section.severity.kind);
        appendFieldGroup(root, `${group.label}: ${section.name.replaceAll("_", " ")}`, fields, [...group.sections, i]);
      });
    } else {
      appendFieldGroup(root, group.label, group.fields, []);
    }
    if (group.label === "Book") root.append(buildExcessTable());
  }
}

function appendFieldGroup(root, title, fields, prefix) {
  const heading = document.createElement("h3");
  heading.textContent = title;
  root.append(heading);
  for (const f of fields) {
    const path = [...prefix, ...f.path];
    const label = document.createElement("label");
    label.title = f.tip;
    label.append(f.label, numberInput(getPath(preset, path), path));
    root.append(label);
  }
}

function buildExcessTable() {
  const wrap = document.createElement("div");
  const heading = document.createElement("h4");
  heading.textContent = "Excess choices (value, weight)";
  heading.title = "Discrete set of available excesses with weights.";
  const rows = document.createElement("div");
  rows.id = "excess-rows";
  for (const c of preset.book.excess_choices) rows.append(excessRow(c.value, c.weight));
  const add = document.createElement("button");
  add.type = "button";
  add.textContent = "Add row";
  add.addEventListener("click", () => rows.append(excessRow(0, 0)));
  wrap.append(heading, rows, add);
  return wrap;
}

function excessRow(value, weight) {
  const row = document.createElement("div");
  row.className = "excess-row";
  const remove = document.createElement("button");
  remove.type = "button";
  remove.textContent = "✕";
  remove.title = "Remove row";
  remove.addEventListener("click", () => row.remove());
  row.append(numberInput(value), numberInput(weight), remove);
  return row;
}

function collectParams() {
  const params = structuredClone(preset);
  for (const input of $("#params-form").querySelectorAll("input[data-path]")) {
    const path = JSON.parse(input.dataset.path);
    getPath(params, path.slice(0, -1))[path.at(-1)] = Number(input.value);
  }
  params.book.excess_choices = [...$("#excess-rows").children].map((row) => {
    const [value, weight] = row.querySelectorAll("input");
    return { value: Number(value.value), weight: Number(weight.value) };
  });
  return params;
}

// Results left on screen from an earlier run are dimmed and labelled while a
// run is in flight and after a failure: the parameters beside them have moved
// on, so they are no longer what the form describes.
function markStale(stale) {
  const shown = !$("#results").hidden;
  $("#results").classList.toggle("stale", stale && shown);
  $("#stale-note").hidden = !(stale && shown);
}

// A run has no progress to report, so the elapsed second count is the signal
// that the server is still working.
function startElapsed() {
  const status = $("#run-status");
  const started = Date.now();
  const tick = () => {
    status.textContent = `Generating… ${Math.round((Date.now() - started) / 1000)}s elapsed`;
  };
  status.hidden = false;
  tick();
  const timer = setInterval(tick, 1000);
  return () => {
    clearInterval(timer);
    status.hidden = true;
  };
}

let inFlight = null; // AbortController for the run in progress, if any
// The request behind the results on screen. The download sends it again: the
// same seed and parameters reproduce the run byte for byte, so the server
// keeps nothing between the two.
let shownRun = null;

async function generate(event) {
  event.preventDefault();
  clearError();
  if (!preset) { showError("Preset failed to load — reload the page."); return; }
  const btn = $("#generate-btn");
  const cancelBtn = $("#cancel-btn");
  inFlight = new AbortController();
  btn.disabled = true;
  btn.textContent = "Generating…";
  cancelBtn.hidden = false;
  markStale(true);
  const stopElapsed = startElapsed();
  try {
    const body = {
      seed: $("#seed").value,
      start_year: Number($("#start-year").value),
      years: Number($("#years").value),
      initial_book_size: Number($("#initial-book-size").value),
      origin_basis: $("#origin-basis").value,
      preset: presetId,
      params: collectParams(),
    };
    const run = await fetchJSON("/api/generate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: inFlight.signal,
    });
    renderResults(run);
    shownRun = body;
    markStale(false);
  } catch (e) {
    // An abort is the user's own doing, so it is reported as a state, not a
    // failure; the results stay marked stale either way.
    showError(e.name === "AbortError" ? "Run cancelled." : e.message);
  } finally {
    stopElapsed();
    inFlight = null;
    btn.disabled = false;
    btn.textContent = "Generate";
    cancelBtn.hidden = true;
  }
}

// download fetches the zip of the run on screen and hands it to the browser
// as a file.
async function download() {
  if (!shownRun) return;
  clearError();
  const btn = $("#download-btn");
  btn.disabled = true;
  btn.textContent = "Preparing download…";
  try {
    const res = await fetch("/api/download", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(shownRun),
    });
    if (!res.ok) {
      let msg = res.statusText;
      try { msg = (await res.json()).error || msg; } catch { /* not JSON */ }
      throw new Error(msg);
    }
    const blob = await res.blob();
    const name = /filename="([^"]+)"/.exec(res.headers.get("Content-Disposition") || "")?.[1] || "claimsgen.zip";
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = name;
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url));
  } catch (e) {
    showError(`Download failed: ${e.message}`);
  } finally {
    btn.disabled = false;
    btn.textContent = "Download CSVs";
  }
}

function renderResults(run) {
  $("#empty-state").hidden = true;
  $("#results").hidden = false;
  renderRunHeader(run.run);
  renderSummary(run.summary);
  renderTriangles(run.triangles);
  renderDistributions(run.distributions);
  renderRealism(run.realism);
}

function renderRunHeader(run) {
  $("#run-header").textContent =
    `${run.lob} · seed ${run.seed} · ${run.start_year}–${run.start_year + run.years - 1} · ` +
    `${run.origin_basis} origin · ` +
    `${fmtInt.format(run.policies)} policies · ${fmtInt.format(run.claims)} claims · ` +
    `${fmtInt.format(run.transactions)} transactions`;
}

function th(text, tip) {
  const el = document.createElement("th");
  el.textContent = text;
  if (tip) el.title = tip;
  return el;
}

// Column labels for the summary table; the second entry is a hover tip where
// the label alone would mislead.
const SUMMARY_COLUMNS = [
  ["Year"],
  ["Policies"],
  ["Claims"],
  ["Nil claims", "Counted at first close: a nil claim that reopens and then pays still counts here."],
  ["Reopened"],
  ["Earned premium"],
  ["Ultimate (paid)"],
  ["Recovered"],
  ["Loss ratio (gross)"],
];

function renderSummary(summary) {
  const table = document.createElement("table");
  table.className = "data-table";
  const head = table.createTHead().insertRow();
  for (const [label, tip] of SUMMARY_COLUMNS) head.append(th(label, tip));
  const body = table.createTBody();
  for (const row of summary.years) body.append(summaryRow(row, String(row.year)));
  table.createTFoot().append(summaryRow(summary.total, "Total"));
  $("#tab-summary").replaceChildren(table);
}

function summaryRow(row, label) {
  const tr = document.createElement("tr");
  tr.append(th(label));
  const cells = [
    fmtInt.format(row.policies),
    fmtInt.format(row.claims),
    fmtInt.format(row.nil_claims),
    fmtInt.format(row.reopened),
    fmtMoney.format(row.earned_premium),
    fmtMoney.format(row.paid),
    fmtMoney.format(row.recovered),
    row.loss_ratio == null ? "n/a" : row.loss_ratio.toFixed(3),
  ];
  for (const text of cells) {
    const td = document.createElement("td");
    td.textContent = text;
    tr.append(td);
  }
  return tr;
}

// Shared tooltip layer: every value shown on hover is also in a table or
// tooltip-free view, so it enhances rather than gates.
const tooltip = $("#tooltip");
function attachTooltip(el, lines) {
  el.addEventListener("pointermove", (e) => {
    tooltip.replaceChildren(...lines.map((line, i) => {
      const div = document.createElement("div");
      if (i === 0) div.className = "tooltip-value";
      div.textContent = line;
      return div;
    }));
    tooltip.hidden = false;
    tooltip.style.left = `${e.clientX + 12}px`;
    tooltip.style.top = `${e.clientY + 12}px`;
  });
  el.addEventListener("pointerleave", () => { tooltip.hidden = true; });
}

// SVG helpers ---------------------------------------------------------------

const SVG_NS = "http://www.w3.org/2000/svg";

function svgEl(tag, attrs) {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  return el;
}

// Bar with a 4px rounded data-end and a square baseline.
function barPath(x, y, w, h, r) {
  r = Math.min(r, w / 2, h);
  return `M${x},${y + h} L${x},${y + r} Q${x},${y} ${x + r},${y}` +
    ` L${x + w - r},${y} Q${x + w},${y} ${x + w},${y + r} L${x + w},${y + h} Z`;
}

const fmtCompact = new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 });
const compact = (n) => (Math.abs(n) >= 1000 ? fmtCompact.format(n) : String(Math.round(n * 100) / 100));

function chartCard(title) {
  const card = document.createElement("figure");
  card.className = "chart-card";
  const caption = document.createElement("figcaption");
  caption.textContent = title;
  card.append(caption);
  return card;
}

// Triangles tab ---------------------------------------------------------------

// The full sequential blue ramp (reference palette, steps 100 to 700).
const SEQ_RAMP = ["#cde2fb", "#b7d3f6", "#9ec5f4", "#86b6ef", "#6da7ec", "#5598e7",
  "#3987e5", "#2a78d6", "#256abf", "#1c5cab", "#184f95", "#104281", "#0d366b"];

function renderTriangles(triangles) {
  const panel = $("#tab-triangles");
  panel.replaceChildren();
  const toggle = document.createElement("div");
  toggle.className = "toggle";
  let table = triangleTable(triangles.paid);
  const kinds = [["paid", "Paid (gross)"], ["net_paid", "Paid (net)"], ["incurred", "Incurred"]];
  for (const [kind, label] of kinds) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.textContent = label;
    if (kind === "paid") btn.classList.add("active");
    btn.addEventListener("click", () => {
      for (const b of toggle.children) b.classList.toggle("active", b === btn);
      const next = triangleTable(triangles[kind]);
      table.replaceWith(next);
      table = next;
    });
    toggle.append(btn);
  }
  panel.append(toggle, table);
}

function triangleTable(tri) {
  const wrap = document.createElement("div");
  wrap.className = "triangle-wrap";
  const table = document.createElement("table");
  table.className = "data-table triangle";
  const devs = Math.max(...tri.cells.map((row) => row.length));
  const maxVal = Math.max(1, ...tri.cells.flat());
  const head = table.createTHead().insertRow();
  head.append(th("Origin"));
  for (let d = 0; d < devs; d++) head.append(th(d === devs - 1 ? `Dev ${d + 1}+` : `Dev ${d + 1}`));
  const body = table.createTBody();
  tri.cells.forEach((row, i) => {
    const tr = body.insertRow();
    tr.append(th(String(tri.start_year + i)));
    row.forEach((v, d) => {
      const td = tr.insertCell();
      td.textContent = compact(v);
      const step = Math.min(SEQ_RAMP.length - 1, Math.max(0, Math.floor((v / maxVal) * SEQ_RAMP.length)));
      td.style.background = SEQ_RAMP[step];
      // Ink flips to white where the fixed ramp hex turns dark, independent of theme.
      td.style.color = step >= 6 ? "#ffffff" : "#0b0b0b";
      attachTooltip(td, [`$${fmtMoney.format(v)}`, `origin ${tri.start_year + i}, cumulative to dev year ${d + 1}${d === devs - 1 ? "+" : ""}`]);
    });
  });
  // ATA footer: the factor under dev column d develops d to d+1.
  const foot = table.createTFoot().insertRow();
  foot.append(th("ATA"));
  for (let d = 0; d < devs; d++) {
    const td = foot.insertCell();
    const f = tri.ata ? tri.ata[d] : null;
    td.textContent = f == null ? "" : f.toFixed(3);
  }
  wrap.append(table);
  return wrap;
}

// Distributions tab -----------------------------------------------------------

function renderDistributions(d) {
  $("#tab-distributions").replaceChildren(
    histogramCard("Claim severity (ultimate paid, log-spaced bins) · paying claims only", d.severity, (v) => `$${compact(v)}`),
    histogramCard("Report lag (days)", d.report_lag_days, compact),
    histogramCard("Close lag (days)", d.close_lag_days, compact),
  );
}

function histogramCard(title, hist, formatX) {
  const card = chartCard(title);
  const bins = hist.bins || [];
  if (bins.length === 0) {
    const note = document.createElement("div");
    note.className = "empty-note";
    note.textContent = "No data.";
    card.append(note);
    return card;
  }
  const W = 460, H = 200;
  const pad = { top: 12, right: 8, bottom: 24, left: 44 };
  const plotW = W - pad.left - pad.right, plotH = H - pad.top - pad.bottom;
  const svg = svgEl("svg", { viewBox: `0 0 ${W} ${H}`, role: "img" });
  const maxCount = Math.max(1, ...bins.map((b) => b.count));
  const ticks = 4;
  for (let i = 0; i <= ticks; i++) {
    const value = Math.round((maxCount / ticks) * i);
    const y = pad.top + plotH - (value / maxCount) * plotH;
    svg.append(svgEl("line", { x1: pad.left, x2: W - pad.right, y1: y, y2: y, class: "gridline" }));
    const label = svgEl("text", { x: pad.left - 6, y: y + 3, class: "axis-label", "text-anchor": "end" });
    label.textContent = compact(value);
    svg.append(label);
  }
  const slot = plotW / bins.length;
  const barW = Math.min(24, Math.max(1, slot - 2)); // ≤24px thick, 2px surface gap
  bins.forEach((b, i) => {
    const h = (b.count / maxCount) * plotH;
    const x = pad.left + i * slot + (slot - barW) / 2;
    const bar = svgEl("path", { d: barPath(x, pad.top + plotH - h, barW, Math.max(h, 0.5), 4), class: "bar" });
    attachTooltip(bar, [`${fmtInt.format(b.count)} claims`, `${formatX(b.lo)} to ${formatX(b.hi)}`]);
    svg.append(bar);
  });
  svg.append(svgEl("line", { x1: pad.left, x2: W - pad.right, y1: pad.top + plotH, y2: pad.top + plotH, class: "baseline" }));
  const lo = svgEl("text", { x: pad.left, y: H - 6, class: "axis-label" });
  lo.textContent = formatX(bins[0].lo);
  const hi = svgEl("text", { x: W - pad.right, y: H - 6, class: "axis-label", "text-anchor": "end" });
  hi.textContent = formatX(bins.at(-1).hi);
  svg.append(lo, hi);
  card.append(svg);
  return card;
}

// Realism tab -----------------------------------------------------------------

function renderRealism(r) {
  const panel = $("#tab-realism");
  panel.replaceChildren();
  const banner = document.createElement("div");
  if (!r.scored) {
    banner.className = "banner banner-neutral";
    banner.textContent = `Not scored - ${r.note}`;
    panel.append(banner);
    return;
  }
  banner.className = `banner ${r.pass ? "banner-pass" : "banner-fail"}`;
  banner.textContent = r.pass
    ? "✓ Pass - every metric inside the Schedule P P5-P95 reference band"
    : "✗ Fail - some metrics fall outside the Schedule P P5-P95 reference band";
  const scope = document.createElement("p");
  scope.className = "empty-note";
  const ref = r.reference;
  const pool = `The reference companies are the ${ref.companies} with steady premium and reinsurance that write at least $${ref.min_premium / 1e6}m a year.`;
  const names = (r.sections || []).map((s) => s.replaceAll("_", " "));
  const scored = names.length
    ? `Scored on the ${names.join(" and ")} ${names.length > 1 ? "sections" : "section"} alone, their claims against their share of premium: the Schedule P ${ref.label} reference is a liability line, and the preset scores these sections against it. ${pool}`
    : `Scored on the whole book against the Schedule P ${ref.label} reference. ${pool}`;
  scope.textContent = `${scored} The loss ratio band uses each company's loss ratio developed to age 10. The incurred factors compare generated paid plus case with Schedule P incurred less its bulk and IBNR reserves. The drift band is each company's drift over the reference median, which leaves out the market cycle the companies share.`;
  panel.append(
    banner,
    scope,
    bandCard("Paid age-to-age factors vs reference P5-P95 (min/max faint)", r.paid_ata || []),
    bandCard("Incurred age-to-age factors vs reference P5-P95 (min/max faint)", r.incurred_ata || []),
    bandCard("Paid to date as a share of paid at age 10 vs reference P5-P95 (min/max faint)",
      (r.paid_shares || []).map((c) => ({ ...c, label: `age ${c.age}` }))),
    bandCard("Net loss ratio vs Schedule P P5-P95 (min/max faint)", [{ ...r.loss_ratio, label: "Net LR" }]),
    bandCard("Loss-ratio drift 2nd half / 1st half vs reference, relative to its median (flat = 1)", [{ ...r.loss_ratio_drift, label: "Drift" }]),
  );
}

function bandCard(title, checks) {
  const card = chartCard(title);
  if (checks.length === 0) {
    const note = document.createElement("div");
    note.className = "empty-note";
    note.textContent = "No checkable ages.";
    card.append(note);
    return card;
  }
  const W = 460, rowH = 26, padLeft = 64, padRight = 76;
  const svg = svgEl("svg", { viewBox: `0 0 ${W} ${checks.length * rowH + 8}`, role: "img" });
  let lo = Infinity, hi = -Infinity;
  for (const c of checks) {
    lo = Math.min(lo, c.min, c.value);
    hi = Math.max(hi, c.max, c.value);
  }
  const span = hi - lo || 1;
  lo -= span * 0.08;
  hi += span * 0.08;
  const x = (v) => padLeft + ((v - lo) / (hi - lo)) * (W - padLeft - padRight);
  checks.forEach((c, i) => {
    const cy = i * rowH + rowH / 2 + 4;
    const label = svgEl("text", { x: padLeft - 8, y: cy + 3, class: "axis-label", "text-anchor": "end" });
    label.textContent = c.label ?? `${c.age}→${c.age + 1}`;
    const outer = svgEl("rect", {
      x: x(c.min), y: cy - 5, width: Math.max(x(c.max) - x(c.min), 1), height: 10, rx: 5, class: "band-outer",
    });
    const band = svgEl("rect", {
      x: x(c.lo), y: cy - 4, width: Math.max(x(c.hi) - x(c.lo), 1), height: 8, rx: 4, class: "band",
    });
    const dot = svgEl("circle", { cx: x(c.value), cy, r: 5, class: c.within ? "dot" : "dot dot-out" });
    attachTooltip(dot, [
      c.value.toFixed(4),
      `P5-P95 ${c.lo.toFixed(4)} to ${c.hi.toFixed(4)}`,
      `min/max ${c.min.toFixed(4)} to ${c.max.toFixed(4)}`,
    ]);
    const status = svgEl("text", {
      x: W - padRight + 8, y: cy + 3,
      class: `status-label ${c.within ? "status-ok" : "status-out"}`,
    });
    status.textContent = c.within ? "✓ within" : "✗ outside";
    svg.append(label, outer, band, dot, status);
  });
  card.append(svg);
  return card;
}

function initTabs() {
  $("#tabs").addEventListener("click", (e) => {
    const btn = e.target.closest("button[data-tab]");
    if (!btn) return;
    for (const b of $("#tabs").querySelectorAll("button")) b.classList.toggle("active", b === btn);
    for (const panel of document.querySelectorAll(".tab-panel")) panel.hidden = panel.id !== `tab-${btn.dataset.tab}`;
  });
}

$("#config-form").addEventListener("submit", generate);
$("#cancel-btn").addEventListener("click", () => inFlight?.abort());
$("#download-btn").addEventListener("click", download);
$("#reset-params").addEventListener("click", () => {
  if (!preset) { showError("Preset failed to load — reload the page."); return; }
  try { buildParamsForm(); } catch (e) { showError(e.message); }
});
initTabs();
loadLimits().catch((e) => showError(e.message));
loadFields().then(loadLOBs).catch((e) => showError(e.message));
