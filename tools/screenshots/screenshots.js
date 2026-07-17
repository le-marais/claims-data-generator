"use strict";
// Regenerates the README UI screenshots against a running claimsgen UI.
// Usage: `claimsgen ui --port 8093` in one shell, then `npm install &&
// node screenshots.js` here. Requires a local Chrome install.
const puppeteer = require("puppeteer-core");
const path = require("path");

const OUT = path.join(__dirname, "..", "..", "docs", "screenshots");
const URL = process.env.CLAIMSGEN_URL || "http://127.0.0.1:8093";
const CHROME = process.env.CHROME_PATH || "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";

async function generateAndWait(page) {
  await page.click("#generate-btn");
  await page.waitForFunction(
    () => !document.querySelector("#generate-btn").disabled &&
          !document.querySelector("#results").hidden &&
          document.querySelector("#error-banner").hidden,
    { timeout: 120000 }
  );
}

async function shootResults(page, file) {
  const el = await page.$("main.results");
  await el.screenshot({ path: path.join(OUT, file) });
}

async function selectTab(page, tab) {
  await page.click(`#tabs button[data-tab="${tab}"]`);
  await new Promise((r) => setTimeout(r, 300));
}

(async () => {
  const browser = await puppeteer.launch({
    executablePath: CHROME,
    headless: "new",
    args: ["--force-device-scale-factor=1"],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1384, height: 905, deviceScaleFactor: 1 });
  await page.goto(URL, { waitUntil: "networkidle0" });
  await page.waitForFunction(() => document.querySelectorAll("#lob-select option").length > 0);

  await generateAndWait(page);
  await new Promise((r) => setTimeout(r, 300));
  await page.screenshot({ path: path.join(OUT, "ui-summary.png") });

  await selectTab(page, "triangles");
  await shootResults(page, "ui-triangles.png");

  await selectTab(page, "distributions");
  await shootResults(page, "ui-distributions.png");

  await selectTab(page, "realism");
  await shootResults(page, "ui-realism-pass.png");

  // Failing run for the README: base frequency 0.5 pushes the loss ratio
  // outside its band.
  await page.evaluate(() => {
    const input = [...document.querySelectorAll("#params-form input[data-path]")]
      .find((i) => i.dataset.path === JSON.stringify(["claims", "base_frequency"]));
    input.value = "0.5";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await generateAndWait(page);
  await selectTab(page, "realism");
  await shootResults(page, "ui-realism-fail.png");

  await browser.close();
  console.log("done");
})().catch((e) => { console.error(e); process.exit(1); });
