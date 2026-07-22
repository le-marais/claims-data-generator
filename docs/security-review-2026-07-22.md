# Security and vulnerability review - 2026-07-22

Scheduled production-code security review of the `claimsgen` repository.

- Scope: all tracked source, config, docs, and tooling on branch `review-cleanup-sl3-sl4-mf2`, plus the local working tree and git-ignore setup.
- Method: manual code review of every Go source file in the generation and web paths, the web front-end, the CSV/config/reference-data adapters, the tooling scripts, dependency manifests, secret-pattern scans across tracked files, and a sample of the reference data.
- Reviewer: automated Claude Code review agent, running locally.

## Executive summary

Overall risk rating: **low**.

`claimsgen` is a local, single-user Go CLI plus an optional loopback-only browser UI that generates fully synthetic insurance claims data. The attack surface is intentionally small and the code is written defensively. The review found no exploitable remote vulnerabilities, no secrets, and no real or personal data. The reference data is public NAIC Schedule P aggregate loss triangles keyed by company code - integer dollar amounts, no PII.

The findings below are hardening and hygiene items, not remotely exploitable defects. The single most important caveat is that the low rating depends on the tool staying loopback-only and single-user. The roadmap mentions opening the tool to the wider actuarial community - if that ever means exposing the UI beyond localhost, two low findings (unbounded output path, unbounded run parameters) become high or critical. See the roadmap remarks.

This rating reflects the tool as designed and run today: local, loopback, synthetic output.

### Findings by severity

| ID | Severity | Finding | Location |
| --- | --- | --- | --- |
| M1 | Low (high if exposed) | `/api/generate` writes to a fully caller-controlled output path with no base-directory confinement; empty-Origin requests bypass the CSRF guard | `internal/infrastructure/web/server.go:56`, `:107` |
| L1 | Low | No upper bounds on numeric run parameters (`years`, `initial_book_size`) in the web handler - local resource exhaustion | `internal/infrastructure/web/server.go:131` |
| L2 | Low | Claude Code and superpowers ignore rules live in `.git/info/exclude` (local only), not the tracked `.gitignore` - a fresh clone would not ignore local session artifacts | `.gitignore`, `.git/info/exclude` |
| L3 | Low | Screenshot dev tool uses an unpinned `puppeteer-core` (`^24.0.0`) with no committed lockfile - non-reproducible, no integrity pinning | `tools/screenshots/package.json:6`, `.gitignore:5` |
| I1 | Info | API error responses echo raw error strings including absolute filesystem paths | `internal/infrastructure/web/server.go:141`, `:159` |
| I2 | Info | No automated dependency or vulnerability scanning (`govulncheck`) and no CI in the repo | repo-wide |
| I3 | Info | CSV writer relies on an all-numeric/enum field invariant to avoid CSV formula injection - safe today, must be preserved | `internal/infrastructure/csv/writer.go:20` |

## Detailed findings

### M1 - Unbounded output path in the generate endpoint (low; high if exposed)

`internal/infrastructure/web/server.go:107` handles `POST /api/generate`. It takes `out_dir` verbatim from the request body, resolves it with `filepath.Abs`, and passes it to `csvout.WriteDataset`, which calls `os.MkdirAll(dir, 0o755)` and `os.Create` for `policies.csv`, `claims.csv`, and `transactions.csv` (`internal/infrastructure/csv/writer.go:17`). There is no confinement to a base directory and no rejection of absolute paths or `..` escapes, so the endpoint can create directories and overwrite those three fixed filenames anywhere the process user can write.

The Origin/Host guards (`server.go:48`) block browser-driven cross-site and DNS-rebinding attacks, and the listener is loopback-only (`cmd/claimsgen/main.go:121`), so today the capability is limited to the local user driving their own UI - the same privilege they already have. Two things still make this worth fixing:

- The CSRF guard trusts an absent Origin: `if origin := r.Header.Get("Origin"); origin != "" && !localOrigin(origin)` (`server.go:56`). Any local client that omits the Origin header (a non-browser process) passes the check and can drive file writes.
- If the server is ever exposed beyond loopback (see roadmap), this becomes an unauthenticated arbitrary-write primitive.

Recommendation: confine `out_dir` to a server-configured base directory, reject absolute paths and paths that escape the base after `filepath.Clean`, and do not treat an empty Origin as trusted for state-changing requests. Keep the loopback bind.

### L1 - No bounds on numeric run parameters (low)

`server.go:131` passes `StartYear`, `Years`, and `InitialBookSize` from the request straight into `application.GenerateDataset`. A large `initial_book_size` or `years` drives unbounded memory, CPU, and disk usage - a local denial of service. The 1 MB body cap (`server.go:108`) does not help, since a small body can request a huge run.

Recommendation: validate sane upper bounds on `years` and `initial_book_size` in the handler before generation, and reject non-positive values with a 400.

### L2 - Ignore rules for local agent artifacts are not in the tracked gitignore (low)

`.claude/` and `.superpowers/` are correctly ignored on this machine, but the rules come from `.git/info/exclude` (which lists `.claude/scheduled_tasks.lock`, `.claude/scheduled_tasks.json`, `.claude/routines/.state/`, `.claude/worktrees/`, `.claude/mailbox/`, `.claude/agent-registry.json`, and others) and from `.superpowers/sdd/.gitignore`. `.git/info/exclude` is local to this clone and does not travel with the repository. The tracked `.gitignore` only covers `/output/`, `/claimsgen`, `*.exe`, and two `tools/screenshots/` paths.

A different contributor cloning the repo and using Claude Code would generate `.claude/` session artifacts that are not ignored, risking accidental commit of local session state (prompts, scheduled-task metadata, agent memory). No such artifact is currently tracked - this is preventive.

Recommendation: add `.claude/` and `.superpowers/` (or the specific artifact patterns) to the committed `.gitignore` so the protection travels with the repo.

### L3 - Unpinned screenshot tool dependency, no committed lockfile (low)

`tools/screenshots/package.json:6` pins `puppeteer-core` as `^24.0.0` (a floating caret range), and `.gitignore:5` excludes `tools/screenshots/package-lock.json`. With no committed lockfile, `npm install` resolves a floating version with no integrity pinning, so the dev tool is non-reproducible and exposed to a malicious minor or patch release of puppeteer-core or its transitive tree.

This is a manually run, developer-only tool that launches a local Chrome, so blast radius is limited to a developer machine. Still, it is the one supply-chain weakness in the repo.

Recommendation: commit `tools/screenshots/package-lock.json` (remove it from `.gitignore`) and pin puppeteer-core to an exact version, so screenshot regeneration is reproducible and integrity-checked.

### I1 - Error responses leak local filesystem paths (info)

`writeError` (`server.go:159`) returns `err.Error()` directly to the client for config-parse and filesystem errors (`server.go:141`), which can include absolute paths from `os.MkdirAll` / `os.Create`. On a local single-user tool this is low impact, but it is unnecessary detail in an API response. Recommendation: return generic messages for internal errors and log the detail server-side.

### I2 - No automated vulnerability scanning or CI (info)

The repo has no `.github/workflows` and no evidence of `govulncheck` or dependency scanning. `AGENTS.md` documents `go test` and `go vet` as the gate, which does not catch known-vulnerable dependencies. Recommendation: add `govulncheck ./...` to the local workflow and to CI when CI is introduced, so advisories against the Go toolchain, gonum, or yaml.v3 surface automatically.

### I3 - CSV formula-injection invariant (info)

`internal/infrastructure/csv/writer.go:20` documents that every emitted field is numeric, an ISO-8601 date, or a fixed enum, so no field can contain a comma, newline, or a spreadsheet formula lead character, and plain `fmt.Sprintf` is safe. This is correct today. The risk is future drift: if a free-text column (for example a claim description or a class name) is ever added, the output becomes vulnerable to CSV/formula injection when opened in a spreadsheet. Recommendation: keep the note, and switch to `encoding/csv` with formula-lead-character escaping the moment any free-text column is introduced.

## What is solid (verified good posture)

These were checked and found sound - noted so they are not regressed later.

- Network exposure: the UI binds `127.0.0.1` only (`cmd/claimsgen/main.go:121`), guards against DNS rebinding via a loopback Host check and against CSRF via a loopback Origin check (`server.go:48`).
- Request hardening: 1 MB body cap (`http.MaxBytesReader`), `DisallowUnknownFields` on the JSON decoder (`server.go:108`), and strict `KnownFields(true)` YAML decoding (`internal/infrastructure/config/config.go:124`).
- No DOM XSS: the front-end builds every node with `createElement` and `textContent` and never uses `innerHTML` or equivalent sinks (`internal/infrastructure/web/static/app.js`). The one interpolated URL uses `encodeURIComponent` (`app.js:106`).
- No path traversal in static serving: assets are served from an embedded `fs.Sub` tree via `http.FileServerFS` (`server.go:23`, `:39`), not the live filesystem.
- Randomness is not security-sensitive: `internal/infrastructure/random/source.go` uses `math/rand/v2` PCG seeded from a SHA-256 of the seed. This is a simulation RNG chosen for reproducibility (byte-identical output from a seed is a hard project invariant), not for any security purpose. Weak-RNG concerns do not apply - there are no tokens, passwords, or keys generated anywhere in the code.
- No secrets: pattern scans for keys, tokens, passwords, connection strings, and private keys across tracked files returned nothing; there are no `.env`, `.pem`, `.key`, or credential files in the tree; the 10 MB `claimsgen` binary is git-ignored, not tracked.
- No PII: sampled reference JSON is public NAIC Schedule P aggregate paid/incurred triangles keyed by `ClassId`, integer dollar amounts only. `AGENTS.md:13` states the output is fully synthetic with no data-governance concerns, which the review confirms.
- Dependencies: two direct dependencies, both current - `gopkg.in/yaml.v3 v3.0.1` (includes the CVE-2022-28948 panic fix) and `gonum.org/v1/gonum v0.17.0`, on Go 1.26. `go.sum` is present and pins hashes.
- The PowerShell prune tool (`tools/prune-dec2025.ps1`) is careful: dry-run by default, `-LiteralPath` throughout, regex-validated input lines, and it only deletes git-tracked JSON, so an accidental run is recoverable.

## Claude Code setup

- No committed Claude Code configuration. There is no tracked `.claude/settings.json`, no committed hooks, and no committed MCP server config, so the repo grants no auto-approve permissions and ships no code-executing hooks. This is a good default posture - nothing in the repo can silently widen an agent's permissions on clone.
- The only committed agent-facing artifact is `AGENTS.md`. Its guidance is benign (fetch at session start, respect the DDD layering, run `go test` and `go vet` before claiming done) and contains no dangerous instructions. It correctly frames the data-governance position.
- Local `.claude/` state is untracked and correctly excluded via `.git/info/exclude` (see L2). The one live artifact observed, `.claude/scheduled_tasks.lock`, is a runtime lock holding a session id and pid - a local-only artifact, not committed, not sensitive beyond this machine.
- Caveat (repeated as L2): the exclusion is local. If this repo is shared, the ignore rules should be committed so a collaborator's Claude Code session state is not accidentally committed.

## Dev environment

Assessed only from what the repo and working tree reveal.

- Ignore hygiene: covered in L2 and L3. The tracked `.gitignore` is minimal and leans on local-only excludes for the agent artifacts and on a git-ignored npm lockfile.
- No CI/CD: there is no `.github/workflows` or other pipeline config, so there are no CI secrets or pipeline supply-chain surface to review, but also no automated test, vet, or vulnerability gate (I2).
- `.gitattributes` normalizes text to LF and marks `*.png` binary - correct, and it avoids CRLF-related diff and tooling surprises. No security relevance beyond that.
- Platform note: the working tree is on a Windows drive mounted into WSL2 (`/mnt/c`), where all files report `0777`. Those permission bits are a DrvFs artifact, not a real access grant, so the loose-looking modes are not a finding. Be aware that Unix permission assumptions (for example the `0o755` in the CSV writer) are not enforced on this mount.

Blind spot (explicit): this review runs locally and can inspect committed files and the working tree only. It cannot audit the host machine itself - installed toolchain integrity, other running processes, the actual Claude Code `settings.local.json` (excluded and unread), shell history, network configuration, or any credentials in the user's home directory. Those are out of scope for a repo-level review and would need host-level tooling.

## Roadmap remarks

Reviewed `docs/roadmap.md` and the in-flight design docs for SL-3/SL-4 (own-damage severity rework) and MF-2 (trailing partial accident year).

- SL-3/SL-4 and MF-2 carry no security impact. Both are pure statistical model changes - capping own-damage loss at the sum insured, removing a double-counted inflation trend, and windowing claim occurrences to the run period. They add no new inputs, no new I/O, and no new attack surface, and they preserve the existing determinism invariant. The determinism and shift-free properties they protect are correctness concerns, not security ones. Nothing to flag before implementation.
- The security-relevant roadmap item is the stated intent to add a second line of business and then "open the tool to the wider actuarial community" (`docs/roadmap.md:22`, `:29`). If "open" ever means running the UI beyond loopback - a shared or hosted instance - the current threat model no longer holds. In that scenario, M1 (attacker-controllable `out_dir` write) and L1 (unbounded run parameters) escalate to high or critical, and I1 (path-leaking errors) becomes a real information leak.
- Recommendation for that transition: before any non-loopback deployment, add authentication, confine `out_dir` to a server-configured base directory, enforce upper bounds on run parameters, add a per-request generation and concurrency cap, and stop echoing raw errors. Design these in when the "second line of business" plumbing is touched, rather than retrofitting after exposure.
- The "valuation-date extract" and "payment-date inflation" items are output and model options with no new external input surface, so they carry no security concern of their own.

## Prioritized recommendations

1. Confine `out_dir` to a base directory and stop trusting empty Origin for state-changing requests (M1).
2. Bound `years` and `initial_book_size` in the web handler (L1).
3. Move `.claude/` and `.superpowers/` ignore rules into the tracked `.gitignore` (L2).
4. Commit a pinned `package-lock.json` for the screenshot tool and pin `puppeteer-core` (L3).
5. Add `govulncheck ./...` to the workflow, and to CI when CI is introduced (I2).
6. Return generic API error messages; log detail server-side (I1).
7. Treat the "open to the community" roadmap step as a security milestone: auth, path confinement, parameter and resource limits before any non-loopback deployment.
