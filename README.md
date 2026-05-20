# loupe

A local code-review tool that hands your review off to an agent.

Run `loupe` inside a git repo and it starts a local web app. Pick a branch, view
the diff against its base (GitHub-style), and leave line- or file-level comments.
Submit the comments together as a review and an agent — the Claude Code CLI,
spawned headless — researches and acts on them. Choose how it responds:

- **Document** — writes a markdown implementation plan to `.loupe/plans/`.
- **Beads** — creates a `bd` epic with a child issue per cluster of comments.
- **Direct** — edits the working tree to address the feedback.

The agent's session streams live into the browser.

## Architecture

```
cmd/loupe            entrypoint: find git root, serve, open browser
internal/git         branch list, merge-base, unified diff (shells out to git)
internal/store       SQLite (modernc, cgo-free): reviews + comments
internal/agent       spawn `claude -p --output-format stream-json`, parse events
internal/adapter     per-mode prompt + tool allowlist (beads | document | direct)
internal/server      chi HTTP API + WebSocket fan-out; embeds the built SPA
web                  Vite + React: branch picker, diff viewer, agent console
```

The frontend builds into `internal/server/web_dist/` and is embedded via
`go:embed`, so a release is a single static binary.

## Prerequisites

- Go 1.23+
- Node 20+ (to build the frontend)
- `claude` CLI on PATH and authenticated
- `bd` on PATH (only for Beads mode)

## Build & run

```sh
make build     # builds the SPA + the binary
./loupe        # run inside any git repo
```

## Develop (live reload)

```sh
# terminal 1 — backend on a fixed port
LOUPE_ADDR=127.0.0.1:7878 go run ./cmd/loupe

# terminal 2 — Vite dev server (proxies /api + /ws to :7878)
cd web && npm run dev
```

## Status

Working skeleton. Known next steps:

- Anchor comments to `blob_sha` (carried in the store schema but not yet populated
  from the diff) so they survive recomputation.
- Interactive follow-ups: the agent already captures `session_id`; wire a chat box
  to `claude --resume` for back-and-forth.
- Forward permission prompts to the browser via an MCP `canUseTool` server instead
  of the blanket `--allowedTools` allowlist.
- Upgrade the diff viewer to `react-diff-view` for split view and word-level diffs.
