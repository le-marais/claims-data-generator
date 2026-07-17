"use strict";

const $ = (sel) => document.querySelector(sel);

// Field metadata for the LoB parameter form. Paths address the preset JSON;
// labels and tips restate the YAML comments of the preset files.
const FIELD_GROUPS = [
  {
    label: "Book",
    fields: [
      { path: ["book", "growth_factor"], label: "Growth factor", tip: "Year-on-year trend in policy count." },
      { path: ["book", "size_volatility"], label: "Size volatility", tip: "Sigma of the mean-1 lognormal noise on book size." },
      { path: ["book", "spread"], label: "Spread", tip: "Heterogeneity: sigma of sum insured and sd of the risk factor." },
      { path: ["book", "sum_insured_median"], label: "Sum insured median", tip: "Year-1 median sum insured in dollars." },
      { path: ["book", "sum_insured_inflation"], label: "Sum insured inflation", tip: "Annual multiplicative drift of the median." },
      { path: ["book", "premium_rate_factor"], label: "Premium rate factor", tip: "Premium = sum insured x rate x risk factor." },
    ],
  },
  {
    label: "Claims",
    fields: [
      { path: ["claims", "base_frequency"], label: "Base frequency", tip: "Expected reported claims per policy-year." },
      { path: ["claims", "report_lag_median"], label: "Report lag median", tip: "Median occurrence-to-report lag in days." },
      { path: ["claims", "report_lag_sigma"], label: "Report lag sigma", tip: "Sigma of the lognormal report lag." },
      { path: ["claims", "severity", "third_party_weight"], label: "Third party weight", tip: "Probability a claim is third party." },
      { path: ["claims", "severity", "own_damage_median_fraction"], label: "Own damage median fraction", tip: "Median loss as a fraction of sum insured." },
      { path: ["claims", "severity", "own_damage_sigma"], label: "Own damage sigma", tip: "Sigma of the own damage lognormal." },
      { path: ["claims", "severity", "third_party_scale"], label: "Third party scale", tip: "Pareto scale (minimum) in dollars." },
      { path: ["claims", "severity", "third_party_alpha"], label: "Third party alpha", tip: "Pareto tail index; must exceed 1." },
      { path: ["claims", "close_lag", "shape"], label: "Close lag shape", tip: "Gamma shape of the close lag." },
      { path: ["claims", "close_lag", "mean_days"], label: "Close lag mean days", tip: "Base mean report-to-close lag." },
      { path: ["claims", "close_lag", "size_threshold"], label: "Close lag size threshold", tip: "Initial estimate above which the lag stretches." },
      { path: ["claims", "close_lag", "size_multiplier"], label: "Close lag size multiplier", tip: "Stretch factor for large claims." },
      { path: ["claims", "close_lag", "risk_loading"], label: "Close lag risk loading", tip: "Exponent on the policy risk factor." },
      { path: ["claims", "inflation", "mean"], label: "Claims inflation", tip: "Average annual claims inflation factor, applied by occurrence year (1.0 = flat)." },
      { path: ["claims", "nil_probability"], label: "Nil claim probability", tip: "Probability a claim closes without payment; 0 switches nil claims off." },
      { path: ["claims", "reopening", "probability"], label: "Reopen probability", tip: "Chance a closed claim reopens once; 0 switches reopening off." },
      { path: ["claims", "reopening", "estimate_factor"], label: "Reopen estimate factor", tip: "Mean reopen case estimate as a factor of the original initial estimate." },
    ],
  },
  {
    label: "Recoveries",
    fields: [
      { path: ["claims", "recoveries", "salvage", "probability"], label: "Salvage probability", tip: "Chance an own-damage claim yields salvage; 0 switches salvage off." },
      { path: ["claims", "recoveries", "salvage", "mean_share"], label: "Salvage mean share", tip: "Average salvage recovery as a share of the claim's gross paid." },
      { path: ["claims", "recoveries", "subrogation", "probability"], label: "Subrogation probability", tip: "Chance an own-damage claim is subrogated; 0 switches subrogation off." },
      { path: ["claims", "recoveries", "subrogation", "mean_share"], label: "Subrogation mean share", tip: "Average subrogation recovery as a share of the claim's gross paid." },
    ],
  },
  {
    label: "Runoff",
    fields: [
      { path: ["runoff", "case_adequacy_mean"], label: "Case adequacy mean", tip: "Mean of ultimate over initial estimate." },
      { path: ["runoff", "case_adequacy_sigma"], label: "Case adequacy sigma", tip: "How wrong individual initial estimates are." },
      { path: ["runoff", "payments_per_year"], label: "Payments per year", tip: "Poisson intensity of interim payments." },
      { path: ["runoff", "settlement_share"], label: "Settlement share", tip: "Fraction of ultimate paid at close." },
      { path: ["runoff", "concentration"], label: "Concentration", tip: "Dirichlet concentration across interim payments." },
      { path: ["runoff", "revisions_per_year"], label: "Revisions per year", tip: "Poisson intensity of pure case revisions." },
      { path: ["runoff", "revision_sigma"], label: "Revision sigma", tip: "Initial sigma of revision noise." },
    ],
  },
];

const fmtInt = new Intl.NumberFormat("en-US");
const fmtMoney = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 });

let preset = null; // defaults for the selected LoB, as served by the API

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

async function loadPreset(id) {
  preset = await fetchJSON(`/api/lobs/${encodeURIComponent(id)}/preset`);
  buildParamsForm();
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

function buildParamsForm() {
  const root = $("#params-form");
  root.replaceChildren();
  for (const group of FIELD_GROUPS) {
    const heading = document.createElement("h3");
    heading.textContent = group.label;
    root.append(heading);
    for (const f of group.fields) {
      const label = document.createElement("label");
      label.title = f.tip;
      label.append(f.label, numberInput(getPath(preset, f.path), f.path));
      root.append(label);
    }
    if (group.label === "Book") root.append(buildExcessTable());
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

async function generate(event) {
  event.preventDefault();
  clearError();
  const btn = $("#generate-btn");
  btn.disabled = true;
  btn.textContent = "Generating…";
  try {
    const body = {
      lob_id: $("#lob-select").value,
      seed: Number($("#seed").value),
      start_year: Number($("#start-year").value),
      years: Number($("#years").value),
      initial_book_size: Number($("#initial-book-size").value),
      out_dir: $("#out-dir").value,
      params: collectParams(),
    };
    const run = await fetchJSON("/api/generate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    renderResults(run);
  } catch (e) {
    showError(e.message);
  } finally {
    btn.disabled = false;
    btn.textContent = "Generate";
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
    `${fmtInt.format(run.policies)} policies · ${fmtInt.format(run.claims)} claims · ` +
    `${fmtInt.format(run.transactions)} transactions · ${run.out_dir}`;
}

function th(text) {
  const el = document.createElement("th");
  el.textContent = text;
  return el;
}

function renderSummary(summary) {
  const table = document.createElement("table");
  table.className = "data-table";
  const head = table.createTHead().insertRow();
  for (const label of ["Year", "Policies", "Claims", "Nil claims", "Reopened", "Earned premium", "Ultimate (paid)", "Recovered", "Loss ratio"]) {
    head.append(th(label));
  }
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
  for (let d = 0; d < devs; d++) head.append(th(`Dev ${d + 1}`));
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
      attachTooltip(td, [`$${fmtMoney.format(v)}`, `origin ${tri.start_year + i}, cumulative to dev year ${d + 1}`]);
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
    histogramCard("Claim severity (ultimate paid, log-spaced bins)", d.severity, (v) => `$${compact(v)}`),
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
  banner.className = `banner ${r.pass ? "banner-pass" : "banner-fail"}`;
  banner.textContent = r.pass
    ? "✓ Pass - every metric inside the Schedule P reference bands"
    : "✗ Fail - some metrics fall outside the Schedule P reference bands";
  panel.append(
    banner,
    bandCard("Paid age-to-age factors vs reference band", r.paid_ata || []),
    bandCard("Incurred age-to-age factors vs reference band", r.incurred_ata || []),
    bandCard("Ultimate loss ratio vs reference band", [{ ...r.loss_ratio, label: "ULR" }]),
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
    label.textContent = c.label ?? `${c.age + 1}→${c.age + 2}`;
    const band = svgEl("rect", {
      x: x(c.min), y: cy - 5, width: Math.max(x(c.max) - x(c.min), 1), height: 10, rx: 5, class: "band",
    });
    const dot = svgEl("circle", { cx: x(c.value), cy, r: 5, class: c.within ? "dot" : "dot dot-out" });
    attachTooltip(dot, [c.value.toFixed(4), `band ${c.min.toFixed(4)} to ${c.max.toFixed(4)}`]);
    const status = svgEl("text", {
      x: W - padRight + 8, y: cy + 3,
      class: `status-label ${c.within ? "status-ok" : "status-out"}`,
    });
    status.textContent = c.within ? "✓ within" : "✗ outside";
    svg.append(label, band, dot, status);
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
$("#reset-params").addEventListener("click", buildParamsForm);
initTabs();
loadLOBs().catch((e) => showError(e.message));
