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
  const body = await res.json();
  if (!res.ok) throw new Error(body.error || res.statusText);
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
  for (const label of ["Year", "Policies", "Claims", "Earned premium", "Ultimate (paid)", "Loss ratio"]) {
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
    fmtMoney.format(row.earned_premium),
    fmtMoney.format(row.paid),
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

// Chart tabs - implemented in the charts change; stubs keep renderResults total.
function renderTriangles(t) {}
function renderDistributions(d) {}
function renderRealism(r) {}

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
