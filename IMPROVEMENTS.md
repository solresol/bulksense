# taxowalk — Improvements (2026-07-11)

taxowalk is a Go CLI (v0.2.9) that classifies free-form product descriptions into the Shopify product taxonomy by iteratively prompting OpenAI's `gpt-5.6-luna`, walking one taxonomy level at a time until a leaf or "none of these". It ships companion tools (`taxoname`, `taxopath`, `taxowalk-report`), man pages, and a full multi-platform release pipeline (deb/apt, macOS, Windows). The tree is clean, tests exist for every internal package (~1,000 test lines against ~1,600 source lines), and recent commits added schema-based function calls, retry logic, and GPG repo signing. Overall a healthy small project; the items below are the highest-value gaps.

## Bugs & Fixes

- **README install instructions contradict the signed repo.** Commit `5475937` implemented GPG signing of the apt repository, but `README.md` (line ~29) still tells users to add the repo with `[trusted=yes]` over plain `http://`. Update to `[signed-by=/usr/share/keyrings/...]` with a documented key-fetch step, and prefer `https://` if the host supports it. As written, the signing work provides no protection to anyone following the README.
- **Model name is hardcoded** in `NewOpenAIModel` (`internal/llm/openai.go`, `model: "gpt-5.6-luna"`). When the model is deprecated the tool breaks and requires a code change + version bump (this already happened once — see commit `0acb529`). Add a `--model` flag / `WithModel` option so users can override without a release.
- **Retry policy check**: `maxAttempts = 3` with 1s base delay in `openai.go` — confirm that 3 attempts × per-level calls fits within the 5-minute default `--timeout` for deep taxonomy paths; a deep walk (7+ levels) with retries and backoff can plausibly exceed it. Consider making per-request timeout separate from the overall walk timeout.

## Improvements

- **Batch mode.** `cmd/taxowalk/main.go` handles one description per invocation. Real use (classifying a catalogue) wants `--batch` reading one description per line (or JSONL) and emitting CSV/JSONL, reusing the cached taxonomy and a single HTTP client. This is the single biggest usability win.
- **Cost/latency reduction via memoization.** The classifier re-asks the model from the root for every product. Consider an optional embedding or shortlist pre-filter, or at minimum expose token usage per classification on stdout (`--show-usage`) rather than only via the optional `--history-db` SQLite log (`internal/history/history.go`).
- **Structured output flag** (`--json`) emitting `{id, path, leaf_name, usage}` in one machine-readable blob instead of the current combinatorial `--show-path`/`--show-leaf-name` flags.
- **Taxonomy cache staleness**: `internal/taxonomy/taxonomy.go` uses a 24h `cacheMaxAge`. Consider ETag/If-Modified-Since revalidation instead of a hard TTL so offline use past 24h doesn't fail (verify current behaviour on cache-expired + network-down: it should fall back to the stale cache with a warning).

## Testing

- Good unit coverage exists (`classifier_test.go`, `openai_test.go` with a fake client). Missing: an end-to-end test of `cmd/taxowalk/main.go` flag handling (only `version_test.go` exists) — table-test the flag combinations and stdin mode against a stub server via `--openai-base-url` + `--taxonomy-url` pointing at local files.
- Add a CI job that runs `go vet` and `staticcheck`/`golangci-lint` if `ci.yml` doesn't already.
- A periodic (scheduled) CI smoke test against the live Shopify taxonomy URL would catch upstream schema drift, which has bitten this project before (commit `f9413b7` "Fix taxonomy hierarchy reconstruction").

## Documentation

- Fix the `[trusted=yes]` snippet (above) — highest-priority doc change.
- `APT_REPOSITORY.md` and `README.md` overlap; cross-link them and state which is authoritative.
- Document the taxonomy cache location and the `--refresh-taxonomy` interaction in the man page (`docs/taxowalk.1`) if not already there.
- README says "Default OpenAI API key discovery from `~/.openai.key`" — document expected file permissions (should warn or refuse on world-readable key file; see Security).

## Security

- No committed secrets found in the skimmed files (API key comes from env/`~/.openai.key`/flag — good).
- Consider warning when `~/.openai.key` is group/world-readable.
- Passing `--openai-key` on the command line leaks the key into process listings and shell history; document that env/file are preferred, or accept `--openai-key-file`.
- Replace `[trusted=yes]` apt guidance (see Bugs) and serve packages over HTTPS.

## Housekeeping / Modernization

- `go.mod`: verify the Go toolchain directive is current and dependencies (notably `sashabaranov/go-openai`) are up to date; that library's API churns with OpenAI changes.
- `AGENTS.md` mandates VERSION bumps for binary-affecting changes — consider a CI check that fails a PR touching `internal/`/`cmd/` without a `VERSION` change.
- This is a Go project, so the owner's uv/pyproject preference does not apply; no Python or requirements.txt present.

## Quick Wins

1. Fix the README apt snippet (`[trusted=yes]` → `signed-by`). Five minutes, real security value.
2. Add `--model` flag. Small change in `internal/llm/openai.go` + `cmd/taxowalk/main.go`.
3. Add `--json` output flag.
4. CI guard for VERSION bumps.
