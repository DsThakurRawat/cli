# `entire dispatch` — Design

**Date:** 2026-04-16
**Status:** Draft — awaiting review

## Summary

A new top-level CLI command, `entire dispatch`, that summarizes recent agent work into a dispatch-shaped bullet list.

**Default path — server-side generation.** The CLI calls a new entire.io endpoint, which enumerates checkpoints, consumes the existing per-checkpoint analyses, and (with `--generate`) synthesizes voice-styled prose using Entire's tokens. The same dispatch is also viewable on entire.io's web UI. CLI prints a URL to the web view alongside the rendered output.

**`--local` flag — local generation.** Bypasses the server entirely. CLI enumerates checkpoints from the local `entire/checkpoints/v1` branch, batch-fetches per-checkpoint analyses (read-only), assembles bullets via a fallback chain, and (with `--generate`) calls the user's own LLM. User pays the tokens.

Works on the current repo by default, scales out to explicit local paths via `--repos` (requires `--local`), and to entire GitHub orgs via `--org`.

Primary use case: users who publish a recurring dispatch/newsletter/update (like the [Entire Dispatch](https://entire.io/blog/entire-dispatch-0x0009)) get the bullet material generated from their actual shipped work without manually trawling `git log`, checkpoints, or PRs.

## Motivation

Existing surfaces don't cover this:

- `entire explain` — per-checkpoint deep-dive.
- `entire search` — cloud-backed, query-driven, per-result.
- `entire sessions` — live session management.
- `entire trail` — branch-level work tracking.

None of them produce a **multi-checkpoint rollup** shaped like a dispatch. The server-side checkpoint analysis pipeline (see analysis-endpoints PR) already generates per-checkpoint labels and 2–4 sentence summaries; `entire dispatch` consumes those to produce the rollup locally.

## Non-goals

- Replacing the server-side analysis pipeline — this command consumes it, not duplicates it.
- Publishing the dispatch anywhere (no Discord/Slack/email integration). Output is text to stdout; the user pastes it wherever.
- Editing or approving individual bullets. The user is expected to tweak the output manually if they want to ship it as-is.
- Supporting fully offline/no-login use. Login is hard-required.

## Command surface

```
entire dispatch [flags]
```

### Source flag

| Flag | Default | Description |
|---|---|---|
| `--local` | off | Generate the dispatch locally using the user's own LLM tokens. Without this flag, dispatch is generated server-side by entire.io using its own tokens, and the CLI only renders the structured response. |

### Scope flags

| Flag | Default | Description |
|---|---|---|
| `--since <duration-or-date>` | `7d` | Time window. Accepts Go duration (`7d`, `168h`, `2w`) and git-style strings (`"2 days ago"`, `"1 week"`, `2026-04-09`). Filters on checkpoint `created_at`. |
| `--branches <list>` | current branch | Comma-separated list of branch names to include. Pass `all` (or `*`) to include every branch. Without the flag, defaults to the current branch only. |
| `--repos <path1,path2,…>` | current repo | Comma-separated list of local repo paths. Each must be Entire-enabled; non-enabled paths are warned and skipped. Requires `--local` (the server can't inspect arbitrary local paths). Mutually exclusive with `--org`. |
| `--org <name>` | — | Enumerate every checkpoint you have access to whose repo belongs to this org. Works in both modes; in default mode, the server resolves the org; in `--local` mode, the CLI queries the cloud org-enumeration endpoint to discover checkpoint IDs. Mutually exclusive with `--repos`. |

### Output flags

| Flag | Default | Description |
|---|---|---|
| `--generate` | off | Synthesize bullets into a voice-styled dispatch via LLM (opener + themed sections + bullets + closer). In default mode, the server runs the LLM (Entire's tokens). In `--local` mode, the CLI runs the user's configured LLM (user's tokens). |
| `--voice <value>` | neutral | Voice/tone guidance for `--generate`. Resolution chain: (1) if value matches a built-in preset name (case-insensitive), use the preset; (2) else if value resolves to a readable file on disk, use its content; (3) else treat as a literal string. Built-in presets shipped with the CLI: `neutral` (default), `marvin` (sardonic AI companion, inspired by the Entire Dispatch). In default mode, passed to the server in the request body; in `--local` mode, used directly. |
| `--dry-run` | off | Show what checkpoints, labels, warnings, and counts a real run would produce, without persisting anything or spending LLM tokens. Works with or without `--generate`. **Analysis fetches still happen** (they're cheap, cached per-checkpoint, and required for the preview to show real pending/failed/unknown/access-denied counts). What dry-run actually suppresses: LLM synthesis, persistence to the dispatches table, fingerprint reservation, and dedupe. In default mode, `dry_run: true` in the POST body instructs the server to skip those persistence-/cost-bearing steps and return the preview-shaped response; in `--local` mode, the CLI does the same work locally and skips the LLM call. |
| `--format <text\|markdown\|json>` | `text` | Output format. `text` = plain-text with `•` bullets and indented sections, suitable for terminals. `markdown` = pure markdown (`#` headers, `-` bullets, fenced where appropriate), paste-friendly for docs and blog posts. `json` = structured payload (checkpoints, analyses, labels, files) for pipelines. Rendering happens client-side in both modes — the server always returns the structured payload, and the CLI's renderer produces the requested format. |

### Behavior flags

| Flag | Default | Description |
|---|---|---|
| `--wait` | off | Block until pending/generating analyses complete (5-min cap). Default: skip + warn. Honored in both modes. |

### Auth

Hard require. Without a token, errors with `"run \`entire login\`"`. Same pattern as `entire search`.

### Examples

```
entire dispatch                                              # server-side, current repo
entire dispatch --since "last monday"
entire dispatch --branches main,release --since 14d
entire dispatch --branches all --since 1w
entire dispatch --org entireio --since 1w                    # server-side, whole org
entire dispatch --generate --voice "sardonic AI named Marvin"  # server runs LLM

entire dispatch --local                                      # local, user's tokens
entire dispatch --local --repos ~/Projects/cli,~/Projects/web  # --repos requires --local
entire dispatch --local --generate --voice ~/dotfiles/dispatch-voice.md

entire dispatch --format markdown > dispatch.md
entire dispatch --format json > dispatch.json
entire dispatch --dry-run --since 2w
```

## Data flow

Two paths: server-side (default) and local (`--local`). Both produce the same structured payload shape, which the CLI renders consistently.

```mermaid
flowchart TD
    Start([entire dispatch])
    Auth{Auth token<br/>present?}
    LocalCheck{--local flag?}
    AuthErr[[Error:<br/>run `entire login`]]

    Start --> Auth
    Auth -->|missing| AuthErr
    Auth -->|ok| LocalCheck

    LocalCheck -->|no — default| Server
    LocalCheck -->|yes| Local

    subgraph Server [Server path — Entire's tokens]
        direction TB
        S1[CLI resolves scope:<br/>current repo via git remote,<br/>--since, --branches, --org, --voice]
        S2[POST /api/v1/users/me/dispatches]
        S3[Server:<br/>• enumerate indexed checkpoints<br/>• read per-checkpoint analyses cache<br/>• optional LLM synth with voice<br/>• persist dispatch record]
        S4[Response:<br/>structured payload + web_url]
        S1 --> S2 --> S3 --> S4
    end

    subgraph Local [Local path — user's tokens]
        direction TB
        L1[CLI enumerates checkpoints<br/>from entire/checkpoints/v1<br/>in window, on --branches]
        L2[POST /users/me/checkpoints/analyses/batch<br/>per repo, 200 IDs per call]
        L3[Fallback chain per checkpoint:<br/>1 cloud analysis complete<br/>2 local summarize title<br/>3 commit message<br/>4 omit + count]
        L4{--generate?}
        L5[Call user's LLM via<br/>summarize.ClaudeGenerator]
        L1 --> L2 --> L3 --> L4
        L4 -->|yes| L5 --> LDone
        L4 -->|no| LDone[Bullets ready]
    end

    Server --> Render
    Local --> Render

    Render[Render via --format:<br/>text · markdown · json]
    Render --> UrlCheck{Server mode<br/>+ text/markdown?}
    UrlCheck -->|yes| Url[Append web_url line]
    UrlCheck -->|no| Done
    Url --> Done([Done])
```

### Server-side path (default)

1. CLI resolves the scope locally: determines the current repo's `owner/name` from `git remote` (or uses `--org`), collects `--since`, `--branches`, `--voice`, etc.
2. CLI issues a single call: `POST /api/v1/users/me/dispatches` with body `{repo: "<owner/name>"|null, org: "<name>"|null, since: <ISO>, until: <ISO>, branches: [...]|"all", generate: bool, voice: "<string>"|null, dry_run: bool}`. Server floor/ceil-normalizes `since`/`until` to minute precision before any processing (see Idempotency → API window precision); these normalized values are used for the fingerprint, persistence, and the echoed `window.normalized_since`/`normalized_until` in the response.
3. Server enumerates checkpoints, consumes analyses (already cached per-checkpoint), optionally runs its LLM for `--generate`, persists the result (so entire.io can show it), and returns the structured payload plus a `web_url` pointing at the web view.
4. CLI renders to `--format`, prints the web URL at the end in `text`/`markdown` modes.

Server path requirements:
- New endpoint `POST /api/v1/users/me/dispatches` — see Server-side prerequisites below.
- Server stores dispatches for web display (persistence detail is owned by the server spec, not this one).

### Local path (`--local`)

For each checkpoint in the time window, resolve bullet text and section label via this chain:

1. **Cloud analysis** — `POST /api/v1/users/me/checkpoints/analyses/batch` with `{repoFullName, checkpointIds}`. Note: analysis-pipeline `status: failed` is distinct from an HTTP network failure — a pipeline failure on one checkpoint doesn't abort the command, whereas a network failure does (see Edge cases).

   **The batch response returns a per-ID status, never a bare 404 for the checkpoint.** Deliberate contract — avoids overloading 404 across unrelated conditions. Per-ID statuses:
   - `complete` → use first `summary` block as bullet text, `labels` for section grouping.
   - `pending` or `generating` → skip with per-checkpoint warning; counted and reported at end of output. With `--wait`, poll until complete (5-min cap).
   - `failed` (pipeline error for this checkpoint) → fall through to step 2. Counted under `warnings.failed_count`.
   - `not_visible` → the server knows this checkpoint exists but the caller lacks access. Fall through to step 2 (we still have local data — we enumerated this ID from our own git). Counted under `warnings.access_denied_count`, surfaced separately from other statuses.
   - `unknown` → the server has no record of this checkpoint ID. Fall through to step 2 AND increment `warnings.unknown_count`. This surface is necessary because `unknown` can mean either "never pushed yet" (benign) or "backend record lost / ingestion regression" (serious). Making it silent would let data-loss go undetected. The user-visible line is neutral and observational: `"ℹ N checkpoints not known to the server"` — the user or an admin can investigate if this is suspiciously high.
2. **Local summarize title** — if summarization is enabled (`settings.IsSummarizeEnabled()`) and a `Summary.Title` exists for the checkpoint in `entire/checkpoints/v1/<id>/…/metadata.json`, use it. No labels → checkpoint lands in a flat "Updates" section for its repo.
3. **Commit message subject** — if the checkpoint has an associated commit via the `Entire-Checkpoint` trailer, use the subject line. No labels → "Updates".
4. **None of the above** — omit the checkpoint from output. Count reported at end: `"N uncategorized checkpoints skipped."`

### Cloud calls (local mode only)

- **Batch analyses**: one call per repo, up to 200 IDs. Paginate over 200.
- **Org enumeration** (for `--org --local`): a new endpoint, `GET /api/v1/orgs/:org/checkpoints?since=<iso>&limit=…&cursor=…`, which returns `{checkpoints: [{id, repo_full_name, created_at}], cursor}`. Required only for `--org --local`. The server path uses its own internal enumeration.

### Warnings

Warnings reported by the CLI reflect server-observed conditions only. No local-git heuristics for v1 — see Open questions for "push-state nudging" deferral.

**Per-classification warnings** (each counted separately — no conflation of unrelated conditions):

| Classification | Source of truth | Text-mode line |
|---|---|---|
| `access_denied_count` | API status `not_visible` | `⚠ N checkpoints you no longer have access to` |
| `pending_count` | API status `pending`/`generating` | `⏳ N checkpoints still being analyzed (retry in a minute or use --wait)` |
| `failed_count` | API status `failed` | `✕ N checkpoints failed analysis on the server` |
| `unknown_count` | API status `unknown` | `ℹ N checkpoints not known to the server` |
| `uncategorized_count` | all fallback steps exhausted | `N uncategorized checkpoints skipped` |

In `--format text`/`markdown` these render as separate lines in the warnings block; in `--format json` they live under `warnings.<name>`.

## Output format

### `--format text` output (default, no `--generate`)

Plain, dispatch-shaped. Sections are `<repo-name>/` at the top level, then label-based subsections if cloud analyses supplied labels; otherwise a flat "Updates" block per repo.

```
entire dispatch  (7 days, current repo, current branch)

cli/
  CI & Tooling
    • CI tests no longer hang on TTY detection during local runs.
    • Nightly workflow now fails instead of silently skipping when tag exists.
    • Added Vercel preview deploy config for the frontend.

  Hooks & Messaging
    • Hook system messages reworded from "Powered by Entire" to "Entire CLI".

  Settings
    • Summary provider is persisted to local settings and respected by `entire configure`.

13 checkpoints · 4 branches · 23 files touched
```

### `--generate` output

LLM pass stitches the same bullet data into a voice-styled dispatch. One call to the existing `summarize.ClaudeGenerator` with a dispatch-specific system prompt. Without `--voice`, neutral "product update" voice. With `--voice <file>`, the file's content is injected into the system prompt as voice guidance.

```
Beep, boop. Marvin here. Another week of entropy successfully postponed — this time across the CLI, mostly.

## CI & Tooling
- CI tests no longer hang on TTY detection during local runs.
- Nightly release workflow now fails instead of silently skipping.
- Added Vercel preview deploy config for the frontend.

## Hooks & Messaging
- Hook system messages reworded from "Powered by Entire" to "Entire CLI".

## Settings
- Summary provider is persisted to local settings and respected by `entire configure`.

— Until next week. The heat death of the universe remains roughly on schedule.
```

### `--format markdown` output

Same content as the default text output, but pure markdown — headings with `#`, bullets with `-`, inline code with backticks, no box-drawing characters. Designed to be pasted directly into docs, blog posts, or markdown-rendered Slack/Discord.

```
# entire dispatch

_7 days, current repo, current branch_

## cli

### CI & Tooling
- CI tests no longer hang on TTY detection during local runs.
- Nightly workflow now fails instead of silently skipping when tag exists.
- Added Vercel preview deploy config for the frontend.

### Hooks & Messaging
- Hook system messages reworded from "Powered by Entire" to "Entire CLI".

### Settings
- Summary provider is persisted to local settings and respected by `entire configure`.

---

_13 checkpoints · 4 branches · 23 files touched_
```

### `--format json` output

Structured payload with all source data, suitable for piping and scripting:

```json
{
  "generated_at": "2026-04-16T14:32:00Z",
  "window": {
    "normalized_since": "2026-04-09T00:00:00Z",
    "normalized_until": "2026-04-16T14:33:00Z",
    "first_checkpoint_created_at": "2026-04-09T09:12:00Z",
    "last_checkpoint_created_at": "2026-04-16T13:45:00Z"
  },
  "repos": [
    {
      "full_name": "entireio/cli",
      "path": "/Users/.../cli",
      "sections": [
        {
          "label": "CI & Tooling",
          "bullets": [
            {
              "checkpoint_id": "a3b2c4d5e6f7",
              "text": "CI tests no longer hang on TTY detection during local runs.",
              "source": "cloud_analysis",
              "branch": "main",
              "commit_sha": "700aace0f",
              "files_touched": ["cmd/entire/cli/summarize/summarize.go"],
              "created_at": "2026-04-14T18:23:00Z",
              "labels": ["CI & Tooling"]
            }
          ]
        }
      ]
    }
  ],
  "totals": {"checkpoints": 13, "used_checkpoint_count": 11, "branches": 4, "files_touched": 23},
  "warnings": {
    "access_denied_count": 0,
    "pending_count": 0,
    "failed_count": 0,
    "unknown_count": 0,
    "uncategorized_count": 0
  }
}
```

### `--dry-run` output

Compact listing of what *would* be fetched/generated. No cloud calls, no LLM calls. Honors `--format`: text (default), markdown, or json (a stripped-down JSON payload with just enumerated checkpoint IDs + timestamps + branches).

```
entire dispatch --dry-run  (7 days, current repo, current branch)

Would fetch analyses for 13 checkpoints across 1 repo:

cli/
  a3b2c4d5e6f7  main         2026-04-14  Fix hanging CI tests locally
  b4c5d6e7f8a9  soph/fix     2026-04-14  fix hanging summary tty in local tests
  …

Would NOT call the LLM (no --generate).
```

## Architecture

### New package: `cmd/entire/cli/dispatch/`

```
dispatch.go       # top-level orchestration — dispatches to server or local path based on --local
server.go         # server-mode path: POST /api/v1/users/me/dispatches, parse response
local.go          # local-mode path: enumerate → resolve bullets → group
cloud.go          # shared API client: batch analyses, org enumeration, dispatches endpoint
render.go         # text / markdown / json renderers (format-agnostic, consumes shared types)
generate.go       # local --generate LLM pass via summarize.ClaudeGenerator
fallback.go       # the 3-step fallback chain (cloud → local summary → commit message)
types.go          # internal types: Dispatch, Repo, Section, Bullet — shared between modes
dispatch_test.go
server_test.go
local_test.go
cloud_test.go
render_test.go
generate_test.go
fallback_test.go
```

### Wired into cobra

- New file: `cmd/entire/cli/dispatch.go` (top-level, not inside `dispatch/`) — exports `newDispatchCmd()`, registered in `root.go` alongside `explain`, `search`, `sessions`.

### Reuses (no duplication)

| Existing | Used for |
|---|---|
| `cmd/entire/cli/checkpoint/` | Enumerate checkpoints locally via `GitStore` |
| `cmd/entire/cli/strategy` | `ListCheckpoints()` in the time window |
| `cmd/entire/cli/summarize` | `ClaudeGenerator` for `--generate` pass (new system prompt tuned for dispatch synthesis, not per-session summary) |
| `cmd/entire/cli/search` | Auth token lookup (`auth.LookupCurrentToken`), HTTP client pattern |
| `cmd/entire/cli/auth` | Same |
| `cmd/entire/cli/paths` | `WorktreeRoot`, `ToRelativePath` |
| `cmd/entire/cli/trailers` | Parse `Entire-Checkpoint` from commit messages |
| `cmd/entire/cli/settings` | `IsSummarizeEnabled()` for fallback step 2 |

### Intentionally NOT reused

- `cmd/entire/cli/explain.go` — different shape (per-checkpoint detail view).
- `cmd/entire/cli/sessions.go` — manages live sessions, not historical rollups.

### Server-side prerequisites

- `POST /api/v1/users/me/checkpoints/analyses/batch` — **exists** (see analysis-endpoints PR). Used in `--local` mode. **Authorization required**: server MUST enforce per-checkpoint access (each ID's repo must be visible to the caller). IDs the caller cannot see return status `not_visible`, not `unknown`, so the CLI can distinguish access-denied from truly-unknown.
- `POST /api/v1/users/me/dispatches` — **new, required for default mode**. Takes scope params + generate/voice, returns the structured dispatch payload + `web_url`. Persists result so entire.io web can render. See authorization and idempotency sections below.
- `GET /api/v1/orgs/:org/checkpoints?since=…&cursor=…` — **new, required for `--org --local`** only. Server-mode `--org` uses internal enumeration, so this endpoint isn't blocking for the default path. **Authorization required**: response filters to repos the caller has access to within the requested org; mixed-access orgs return only the visible subset (and never leak repo names/IDs outside that subset).

#### Authorization contract (required for all new endpoints)

**All dispatch-related endpoints MUST enforce per-repo authorization before any work or persistence.** Authentication (a valid login token) is necessary but not sufficient.

**Canonical `authorize(requester, dispatch)` algorithm** — applied identically across read endpoints. Dispatches are **content-addressed cache objects with no ownership model in v1** — no `creator_id` is tracked on rows; anyone with current access to every covered repo can view a dispatch.

```
authorize(requester, dispatch):
  for repo in dispatch.covered_repos:
    if not requester has current access to repo:
      return deny(404)   # live strict check; deny == 404 (don't leak existence)
  allow
```

Per-endpoint applications:

- `POST /api/v1/users/me/dispatches` (create) — caller must currently have access to every repo in the resolved scope. For `repo: "owner/name"` verify current access; for `org: "name"` resolve scope to intersection of org's repos and caller's current access; empty intersection → 404. The persisted row stores `covered_repos` (the authorization boundary) and the content. No `creator_id`.
- `GET /api/v1/dispatches/:id` (detail) — run `authorize()`; on deny, return 404 (don't leak existence).
- Per-repo / per-org dispatch listings — filter to dispatches whose `covered_repos` is a subset of what the viewer currently has access to. Must not leak titles or counts for denied dispatches.
- `GET /api/v1/orgs/:org/checkpoints` — filter results to repos the caller has current access to in that org. Zero access in the org → 404.
- Batch analyses — per-ID access check; inaccessible IDs return `not_visible`, not the content.

**No DELETE endpoint in v1.** Dispatches are cache-like; server GCs old rows by age (retention window e.g. 90 days, finalized in operational docs). Deferred to post-v1 if a concrete user need appears.

**No `creator_id` means**: no personal feed, no "dispatches I made," no creator-only actions. A user navigating to a repo's dispatches page sees every dispatch whose `covered_repos ⊆ user's current access`. If two users submit the same inputs, they get the same cached dispatch record. If a user loses access to a covered repo, they lose the ability to view that dispatch — matches the restrictive-access default.

#### Persistence and idempotency — pure content-addressed cache

**Only `generate: true` dispatches persist.** Bullets-only requests (`generate: false`) are computed live and returned inline with no persisted row, no fingerprint, no dedupe — the bullet data is cheap to regenerate, so caching is unnecessary. `dry_run: true` requests likewise never persist (see Dry-run below). The persistence tier exists to cache LLM-generated prose, which is the expensive step worth caching.

**Implications:**
- Bullets-only responses have no `id`, no `web_url`, no `fingerprint_hash` — same shape as the dry-run response minus the `dry_run: true` flag.
- Users who want to revisit a bullets-only dispatch re-run the command; output will be identical for identical inputs (deterministic transform of a fixed used-ID set).
- Only LLM-generated dispatches show up in the web UI's per-repo / per-org listings.

**API window precision — floor/ceil, not floor/floor**: the server normalizes `since` and `until` to minute precision at request receipt using half-open-interval semantics (`since` floored, `until` ceiled). This preserves all checkpoints the caller asked about and makes sub-minute input variations deterministic.

- Request with `since=2026-04-16T14:32:47.893Z`, `until=2026-04-16T14:33:12.001Z` → normalized `since=2026-04-16T14:32:00Z`, `until=2026-04-16T14:34:00Z`.
- All checkpoints created in the second half of `until`'s original minute are still included (ceil preserves them).

The normalized values appear in the echoed `window` metadata on the response. They're not part of the fingerprint — see below.

**Idempotency key (for `generate: true` requests only)** — a pure content-addressed cache key:

```
fingerprint_hash = SHA-256(
  sha256(lex_sorted(used_checkpoint_ids).join(","))  // the rendered artifact's inputs
  + "|"
  + voice_normalized                                  // preset name; OR hash of voice file content; OR hash of literal string
)
```

That's the entire key. Deliberately excluded:

| Field | Why excluded |
|---|---|
| `user_id` | Dispatches are shared cache objects; no ownership model in v1. Two users submitting the same inputs get the same record. |
| `scope` (repos / org) | Different scope requests that resolve to the same used-ID set produce the same rendered artifact. Dedupe is semantically correct. |
| `normalized_since` / `normalized_until` | Different windows with identical used sets produce identical output. |
| `branches` | Same as scope — a filter on candidates, not on output. |
| `generate` | Bullets-only doesn't persist at all, so there's no generate=false record to collide with. |

**What the fingerprint does and does not trigger as a fresh dispatch:**

| Change | Fresh dispatch? | Why |
|---|---|---|
| New checkpoint enters window AND becomes a bullet | Yes | Joins used set → hash changes |
| Pending checkpoint completes AND becomes a bullet | Yes | Joins used set |
| Used checkpoint revoked/failed/removed | Yes | Leaves used set |
| New checkpoint enters window but stays `pending` | No | Not in used set; cached dispatch returned |
| Analysis pipeline reprocesses a used checkpoint (same id, revised labels/summary) | No | Id unchanged; cached dispatch returned |
| Different request window/scope/branches, same used set | No | Artifact is identical; labels on echoed metadata may be stale |
| Different voice | Yes | Explicit fingerprint component |
| Two different users, same used set + same voice | No | Shared cache object |

**Decision log — explicitly accepted tradeoffs:**

- **Reprocessing staleness**: analysis pipeline re-runs are rare and typically produce incrementally updated content. We accept that a cached dispatch may reflect a slightly older pipeline version for the same ids. If a user wants fresh content, they can modify the voice (shifting the fingerprint) or wait for a new checkpoint to shift the used set.
- **Pending-not-used staleness**: a newly-arrived pending checkpoint doesn't change the dispatch until it completes and joins the used set. The cached row's `pending_count` may be outdated for an intermediate submission, but when the pending transitions to complete, the used set changes and a fresh dispatch is produced.
- **Scope/window label drift**: the echoed `covered_repos`/`window`/`branches` metadata on a deduped response reflects the first submitter's request. Since the content is identical across submissions with the same used set, this is accepted as cosmetic and not worth a cache-key dimension.

**Order of operations on the server** (for `generate: true` requests):

1. Resolve access + enumerate candidate checkpoints in the request's window.
2. Batch-fetch analyses.
3. Apply the fallback chain to determine the "used" set.
4. Compute `fingerprint_hash = sha256(sha256(lex_sorted(used_ids).join(",")) + "|" + voice_normalized)`.
5. **Reserve-before-synthesize** (see below).
6. If reservation won: run LLM, persist `covered_repos`, bullets, generated_text, counts, status=`complete`. Return `deduped: false`.
7. If reservation lost: fetch by fingerprint. If `status: complete`, return `deduped: true`. If `status: generating`, see in-flight coordination.

For `generate: false` requests: steps 1–3 only. Return the bullets payload inline with no persistence. No fingerprint computed.

**Atomic dedupe — reserve the row BEFORE the LLM call**:

The dispatches table has a partial unique index on `fingerprint_hash` scoped to `status IN ('generating','complete')`. Creation is a single transactional upsert:

```sql
INSERT INTO dispatches (id, fingerprint_hash, status, covered_repos, …)
VALUES (gen_id(), :fingerprint_hash, 'generating', :covered_repos, …)
ON CONFLICT (fingerprint_hash)
  WHERE status IN ('generating','complete')
  DO NOTHING
RETURNING id;
```

- **Row returned** → this request won the reservation. It runs the LLM, updates to `status='complete'`, returns.
- **No row returned** → another request already reserved. Fetch the existing row by fingerprint; no LLM call.

**In-flight coordination** — colliding row is still `generating`: server MAY (a) block on a completion signal up to the client's `--wait` limit, or (b) return id with `status: generating` and let the client poll.

**Failure cleanup**: failed synthesis marks the row `status: failed`, which falls outside the partial unique index — a later legitimate retry can reserve afresh. `failed` rows are retained for debugging but don't block new attempts.

**No regenerate endpoint.** Re-submitting `POST /dispatches` with the same inputs naturally returns the same cached dispatch (unchanged used set + voice) or a fresh one (something changed). Nothing else needed.

**Client retry semantics**: CLI can safely retry 5xx on `generate: true`. Atomic upsert guarantees one record per fingerprint.

**Dry-run requests** (`dry_run: true`) skip LLM, persistence, and fingerprinting entirely. Response shape:

```json
{
  "dry_run": true,
  "requested_generate": true,
  "window": {
    "normalized_since": "…",
    "normalized_until": "…",
    "first_checkpoint_created_at": "…",
    "last_checkpoint_created_at": "…"
  },
  "repos": [ /* candidates + used bullets */ ],
  "totals": { "checkpoints": 13, "used_checkpoint_count": 11, "branches": 4, "files_touched": 23 },
  "warnings": { "access_denied_count": 0, "pending_count": 0, "failed_count": 0, "unknown_count": 0, "uncategorized_count": 0 }
}
```

Fields explicitly absent from dry-run responses: `id`, `fingerprint_hash`, `web_url`, `status`, `deduped`, `generated_text`. Dry-run still runs access-resolution + analysis fetches (these determine warnings/counts) but nothing else.

**No DELETE endpoint in v1.** Dispatches are cache-like; server GCs rows older than a retention window (operational default 90 days, finalized in operational docs). A policy-reviewed DELETE can be added post-v1 if a concrete user need emerges.

#### Response shape for `POST /dispatches` (`generate: true` — persisted)

```json
{
  "id": "dsp_01H…",
  "status": "complete",                      /* complete | generating | failed */
  "fingerprint_hash": "sha256:…",            /* echoed so callers can verify / dedupe client-side */
  "deduped": false,                          /* true if server returned existing cached record */
  "web_url": "https://entire.io/dispatch/dsp_01H…",
  "window": {
    "normalized_since": "2026-04-16T14:32:00Z",
    "normalized_until": "2026-04-16T14:34:00Z",
    "first_checkpoint_created_at": "…",
    "last_checkpoint_created_at": "…"
  },
  "covered_repos": ["entireio/cli"],          /* frozen at first synthesis — auth boundary */
  "repos": [ /* same shape as --format json output below */ ],
  "totals": { "checkpoints": 13, "used_checkpoint_count": 11, "branches": 4, "files_touched": 23 },
  "warnings": {
    "access_denied_count": 0,
    "pending_count": 0,
    "failed_count": 0,
    "unknown_count": 0,
    "uncategorized_count": 0
  },
  "generated_text": "…"                        /* present when status: complete */
}
```

#### Response shape for `POST /dispatches` (`generate: false` — NOT persisted, returned inline)

```json
{
  "generate": false,
  "window": {
    "normalized_since": "2026-04-16T14:32:00Z",
    "normalized_until": "2026-04-16T14:34:00Z",
    "first_checkpoint_created_at": "…",
    "last_checkpoint_created_at": "…"
  },
  "repos": [ /* bullets, no covered_repos frozen */ ],
  "totals": { "checkpoints": 13, "used_checkpoint_count": 11, "branches": 4, "files_touched": 23 },
  "warnings": { "access_denied_count": 0, "pending_count": 0, "failed_count": 0, "unknown_count": 0, "uncategorized_count": 0 }
}
```

Fields explicitly absent on a `generate: false` response: `id`, `fingerprint_hash`, `web_url`, `status`, `deduped`, `generated_text`. Nothing was persisted; there is no URL to visit. Same principle as dry-run, just with `"generate": false` (vs `"dry_run": true`) and real data (rather than a preview).

Counts and bullets on a persisted (generate: true) response reflect the snapshot at synthesis time. Because the fingerprint covers the used-ID set + voice only, subsequent requests with the same used set + voice always return this same snapshot. If the caller wants the current state to be re-evaluated (e.g., a pending checkpoint has since completed), they re-submit; if the used set has changed, a fresh dispatch is produced.

**Workflow for adding new entire.io routes** — the `POST /dispatches` and `GET /orgs/:org/checkpoints` endpoints must be developed in a **new** git worktree whose branch is created off the `analysis-chunk-merge` branch (located at `/Users/alisha/Projects/wt/entire.io/analysis-chunk-merge`). Do **not** make changes on the `analysis-chunk-merge` worktree/branch itself — it is used only as the starting point. Also do not add backend routes from within the CLI repo worktree. Coordination: the CLI spec can merge independently of the server endpoints if the CLI is released without the server flag behavior enabled (or gated behind a feature flag until the server ships).

## Edge cases & error handling

| Case | Behavior |
|---|---|
| No checkpoints in window | Print `"No checkpoints in the last Nd."` · exit 0 |
| `--repos` without `--local` | Error: `"--repos requires --local"` |
| `--repos` path isn't a git repo (local mode) | Per-path error with path name, continue with remaining paths |
| `--repos` path is a git repo but not Entire-enabled | Warn + skip, continue |
| `--org` with no indexed checkpoints | Empty output with "try widening `--since`" hint · exit 0 |
| Network error during cloud fetch (either mode) | Abort with clear error. In default mode suggest `--local` as a workaround. |
| Server returns 5xx (default mode) | Retry once with backoff, then abort with `"server error — try --local"` |
| Server returns 404 (default mode — endpoint not deployed) | Clear error: `"dispatch server not available — use --local"` |
| `--wait` timeout (5 min) | Progress ticks while waiting; error out if still incomplete |
| `--generate` with zero eligible checkpoints | Skip LLM call, print same "no checkpoints" message |
| Auth token missing | Error: `"dispatch requires login — run \`entire login\`"` |
| Auth token expired/revoked | Error: `"login expired — run \`entire login\`"` |
| `--repos` and `--org` both passed | Error: `"--repos and --org are mutually exclusive"` |
| `--format json` and `--generate` both passed | Allowed — JSON payload includes a `generated_text` field with the LLM output |
| `--format json` and `--dry-run` both passed | Allowed — JSON-shaped dry-run payload. Dry-run still performs access resolution and analysis fetches (required for accurate warnings/counts) but skips fingerprinting, persistence, dedupe, and LLM synthesis. |
| `--voice` without `--generate` | Error: `"--voice requires --generate"` |
| `--voice <value>` as a file path that looks like a file but can't be read | Error before any cloud calls. If the value has no path separators and doesn't resolve to a file, it's treated as a literal string (no error). |
| `--branches <list>` where a listed branch doesn't exist | Warn per missing branch, continue with remaining branches |
| `--format <value>` not in `text\|markdown\|json` | Error with list of valid values |

## Testing

### Unit (`cmd/entire/cli/dispatch/*_test.go`)

- **Mode selection** (`dispatch_test.go`) — `--local` routes to local path, absence routes to server path. Flag combinations (`--repos` without `--local` → error, `--org` allowed in both).
- **Fallback chain (local mode)** (`fallback_test.go`) — every branch: cloud `complete` → bullet · cloud `pending`/`generating` → skip + `pending_count++` · cloud `unknown` → fall through to local data + `unknown_count++` (explicitly surfaced — see Local path status mapping) · cloud `failed` → fall through + `failed_count++` · cloud `not_visible` → fall through + `access_denied_count++` · local summary present → bullet · local summary absent + commit → bullet · none of above → omit + `uncategorized_count++`. Text-mode rendering assertions for each warning: each non-zero counter produces exactly its documented text-mode line; JSON mode exposes each as `warnings.<name>` at the payload top level.
- **Authorization contract** (server-side tests, not CLI) — `POST /dispatches` with repo/org outside caller's access returns 404; `GET /dispatches/:id` to a user who currently lacks access to any covered_repo returns 404; batch analyses returns `not_visible` for inaccessible IDs.
- **Content-addressed fingerprint** (server-side tests for `generate: true`):
  - (a) Same used-ID set + same voice → `deduped: true`, same id (regardless of caller, window, branches, or scope spelling).
  - (b) Pending→complete transition that joins the used set → hash changes → new dispatch.
  - (c) Used checkpoint revoked / removed / flipped to failed → leaves used set → hash changes → new dispatch.
  - (d) New candidate enters window but stays pending (not used) → hash unchanged → cached dispatch returned (accepted tradeoff — see Decision log).
  - (e) Analysis reprocessing producing different labels/summary on a used id → hash unchanged → cached dispatch returned (accepted tradeoff).
  - (f) Different voice (preset vs preset, preset vs literal, literal vs file) → different fingerprints → separate dispatches.
  - (g) Two different users with the same used set + voice → same dispatch returned to both (shared cache; no `user_id` in key).
  - (h) Same used set + voice but different scope/window/branches requests → same dispatch returned (scope is NOT in key).
  - (i) Multi-repo: union of used IDs across repos produces the same hash regardless of iteration order.
  - (j) **Bullets-only (`generate: false`)** never creates a row: no `id`, no `fingerprint_hash`, no `web_url`, no persistence; test asserts empty dispatches table after N bullets-only POSTs.
  - (k) **Concurrent generate:true** (N parallel POSTs with same fingerprint): exactly one wins the reservation INSERT; exactly one LLM synthesis occurs; all N responses return the same `id`. Race test counts LLM mock invocations = 1 over N concurrent requests.
  - (l) Reservation claimant crashes mid-synthesis: sweeper marks abandoned `generating` rows as `failed`; next retry can reserve afresh.
  - (m) **Window boundary inclusion**: checkpoint created at `14:32:59.500` is included in a request with `until=14:32:47` because `normalized_until` ceils to `14:33:00Z`.
  - (n) **Dry-run response shape** — `dry_run: true` responses include `dry_run: true` and `requested_generate: <echoed boolean>`, and MUST NOT include `id`, `web_url`, `fingerprint_hash`, `status`, `deduped`, or `generated_text`. No dispatches row is created. Both `dry_run+generate:true` and `dry_run+generate:false` variants tested; LLM mock call count = 0 in both.
  - (o) **Bullets-only response shape** — `generate: false` non-dry-run responses include `"generate": false` and MUST NOT include `id`, `web_url`, `fingerprint_hash`, `status`, `deduped`, or `generated_text`. No row created.
  - (p) **Client preview/bullets parsing** — CLI and web frontend branch on `dry_run`/`generate` in the response and never attempt to navigate to `/dispatches/:id` or store an id when no `id` field is present, even if a buggy server leaks one.
- **Server client** (`server_test.go`) — happy path response parsing, 404/5xx mapping to user-facing errors, auth header, request body shape. **Warnings parsing coverage**: for each counter (`access_denied_count`, `pending_count`, `failed_count`, `unknown_count`, `uncategorized_count`), feed a mocked server response with that counter non-zero; assert the client exposes it correctly; assert the text renderer emits exactly the documented line (e.g. non-zero `unknown_count` → `ℹ N checkpoints not known to the server` line appears); assert the JSON passthrough preserves the field at `warnings.<name>`. These tests cover the default (server) mode end-to-end, not only the local-mode fallback.
- **Section grouping** (`render_test.go`) — checkpoints with labels group by label; checkpoints without labels go into flat "Updates"; mixed case (some with labels, some without) — labeled group normally, unlabeled land in "Updates".
- **`--since` parsing** — Go durations, git-style relative, ISO dates, invalid inputs.
- **`--branches` parsing** — single branch, multiple branches, `all`/`*` wildcard, missing branch warnings.
- **`--voice` resolution** — file path that exists, file path that doesn't exist (error), literal string (no path separators), literal string that happens to match a file in cwd (still resolved as file).
- **Render golden files** — `--format text`, `--format markdown`, `--format json`, `--dry-run` (each format), `--generate` (with mocked LLM), web-url trailer in server mode.

### Cloud client (`cloud_test.go`)

- `httptest` server mock. Happy-path batch fetch, 200-ID pagination, 401 → auth error, 5xx → error (no retry), org enumeration with cursor.

### Integration (`cmd/entire/cli/integration_test/dispatch_*.go`)

- Build a test repo with several checkpoints using `testutil`, mock the cloud client, run `entire dispatch`, assert output shape and exit code.
- Cover default (server) mode with mocked server, `--local` mode with mocked analysis endpoint, `--branches`, `--repos` (local only), `--format text|markdown|json`, `--dry-run`.

### Vogon canary (`e2e/tests/dispatch_*.go`)

- Create N checkpoints via the Vogon fake agent over scripted prompts, run `entire dispatch`, assert bullets appear. Runs in `test:ci` — catches prompt parsing and renderer regressions without API cost.

### Real-agent E2E (optional, manual)

- `mise run test:e2e --agent claude-code -run TestDispatch` — runs `entire dispatch --generate` over a recorded fixture, asserts dispatch shape. Gated to avoid token cost in CI.

## Open questions / future work

1. **`--org` endpoint shape** — needs to be designed alongside the API team. Specifically: does it return checkpoint IDs only, or also enough metadata (repo, commit SHA) to skip the per-repo batch fetch? If the former, we do one enumeration call + one batch per repo. If the latter, one call does everything.
2. **Rate limits for large orgs** — an org with 500 checkpoints/week will do 3 batch calls (200 max each). Fine. An org with 10,000 checkpoints/week (large teams) starts hitting rate limits. Probably not an immediate concern; revisit if users report.
3. **Voice preset library** — v1 ships `neutral` and `marvin`. Future candidates: `plain`, `changelog`, `standup`, `release-notes`. Presets are plain markdown files in `cmd/entire/cli/dispatch/voices/`, embedded at build time; adding one is a code-free-ish drop-in. User-defined saved voices (e.g., `~/.config/entire/voices/`) deferred to post-v1.
4. **"Since last dispatch" mode** — currently user has to remember `--since` value. A `--since last-dispatch` mode that reads a timestamp from `.entire/state/last-dispatch` could be nice. Deferred to post-v1.
5. **Additional output formats** — current spec covers `text` (default) and `markdown`. Future candidates: `html` (email-ready), `rss` (for feed-driven newsletters), `slack` (Slack-flavored markdown). Deferred to post-v1.
6. **Local push-state nudging** — v1 does not try to detect or warn about locally-committed-but-unpushed checkpoints. Client-side `git rev-list` against a remote-tracking ref is not authoritative (stale refs, hardcoded `origin`, no upstream), so any such nudge would be unreliable. Could be reintroduced in a later version if users report wanting it, via the branch's configured upstream and an explicit `git fetch` first.
7. **LLM-reshuffle affordance** — with the content-addressed fingerprint, re-submitting `POST /dispatches` returns the cached record when used-ID set + voice are unchanged. The "I want a different LLM wording with exactly the same inputs" case has a workaround today (tweak the voice). If users report wanting an explicit "reshuffle" button, revisit by adding a caller-scoped bypass-dedupe flag. Intentionally deferred — cache semantics are clean without it.
8. **Personal feed / history** — v1 has no per-user dispatch history. Users navigate to repo/org dispatch pages and see all dispatches whose covered_repos they currently have access to. If user feedback asks for "my dispatches" (e.g. "show me what I've submitted recently"), add a lightweight server-side submissions-log (many-to-many between user and dispatch) in a later version. Adding this later is non-breaking.
9. **DELETE** — no DELETE endpoint in v1. Server GC by age (operational default 90 days) is the retention mechanism. If a concrete need emerges (e.g. sensitive content accidentally included), add a policy-reviewed admin-only DELETE or a pull-from-cache operation.

## Interactive wizard

`entire dispatch` with no flags on a TTY auto-enters a `huh`-based wizard. Matches the codebase convention (`entire sessions stop`, `entire search`). With any flag passed, or stdin piped, the wizard is skipped and the command runs non-interactively.

### Trigger rules

- **Wizard ON** — stdin is a TTY AND no flags have been explicitly set.
- **Wizard OFF** — any flag passed (including flags with default-equal values), stdin is piped, or `ACCESSIBLE=1` with no flags (accessible mode uses `NewAccessibleForm()` internally, so wizard still shows but in accessible form).

### Step sequence

1. **Source mode** — Server (default, Entire's tokens) / Local (user's tokens). Maps to `--local`.
2. **Scope** — Current repo / Specific repos (disabled in server mode) / Whole org (prompts for org name) / Custom (jump to freeform flag review).
3. **Time window** — Last 24h / Last 7d (default) / Last 30d / Custom (text input accepting git-style or ISO).
4. **Branches** — Current branch / All branches / Select specific (multi-select from local refs).
5. **Generate prose?** — Bullets only / Generate full dispatch with voice (default in wizard). Maps to `--generate`. If "bullets only", skip step 6.
6. **Voice** (only shown if step 5 = generate) — Neutral product update (default) / Marvin (sardonic AI, Entire Dispatch style) / Custom (text input) / Load from markdown file (file picker).
7. **Format** — Text (default) / Markdown / JSON.
8. **Confirm & run** — Shows the equivalent CLI command, then asks: Run it now (default) / Copy command to clipboard and exit / Edit flags (jumps back to step 1) / Cancel.

Every step has a default that works on Enter; power users can tab through in a few seconds.

### Built-in voice presets

Shipped with the CLI binary via `go:embed`:

```
cmd/entire/cli/dispatch/voices/
  neutral.md       # neutral product-update tone (default)
  marvin.md        # sardonic AI companion, inspired by the Entire Dispatch
```

`--voice` value resolution (unchanged from command surface, repeated here for completeness): preset name (case-insensitive) → file path → literal string.

Adding more presets later: drop a new `.md` into `voices/`, register in a small map. No API changes.

### Accessibility

- All `huh` forms wrapped with the existing `NewAccessibleForm()` helper so `ACCESSIBLE=1` works without extra code per step.
- Escape at any step cancels the wizard cleanly (exit 130, no partial state written).

### Always-print equivalent command

The confirm screen always shows the full CLI command that the wizard assembled. Rationale:

- Users learn the flag syntax organically.
- "Copy command" option lets users drop the command into aliases / CI / scripts without going through the wizard each time.
- Useful for debugging and support ("run `entire dispatch --since 7d --branches main…` and paste the output").

### Wizard-specific tests

- **Step golden files** (`wizard_test.go`) — each step's prompt rendered as a fixture (TUI snapshot).
- **Full flow integration** — scripted stdin drives the wizard through a happy path and asserts the equivalent CLI command output matches expectations.
- **Flag-skip behavior** — any flag passed short-circuits the wizard; verified for each flag.
- **Accessible mode** — `ACCESSIBLE=1` renders plain text prompts, tested with golden output.
- **Cancel paths** — escape at each step cleans up and exits 130.

## Implementation plan

To be produced via the `writing-plans` skill once this spec is approved.
