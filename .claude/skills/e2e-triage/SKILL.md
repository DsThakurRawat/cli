---
name: e2e-triage
description: Triage E2E test failures — download CI artifacts, classify flaky vs real bug, create PRs for flaky fixes and GitHub issues for real bugs
---

# E2E Triage

Automate triage of E2E test failures. Analyze artifacts, classify each failure as **flaky** (agent non-determinism) or **real-bug** (CLI defect), then take action: batched PR for flaky fixes, GitHub issues for real bugs.

## Inputs

The user provides one of:
- **`latest`** — download artifacts from the most recent failed E2E run on main
- **A run ID or URL** — download artifacts from that specific run
- **A local path** — use existing artifact directory (skip download)

## Step 1: Download Artifacts

**If given a run ID, URL, or "latest":**
```bash
artifact_dir=$(scripts/download-e2e-artifacts.sh <input>)
```

**If given a local path:** Use directly, skip download.

## Step 2: Identify Failures

For each agent subdirectory in the artifact root:
1. Read `report.nocolor.txt` — list failed tests with error messages, file:line references
2. Skip agents with zero failures
3. Build failure list: `[(test_name, agent, error_line, duration, file:line)]`

## Step 3: Analyze Each Failure

For each failure, follow the debug-e2e methodology:

1. **Read `console.log`** — what did the agent actually do? Full chronological transcript.
2. **Read test source at file:line** — what was expected?
3. **Read `entire-logs/entire.log`** — any CLI errors, panics, unexpected behavior?
4. **Read `git-log.txt` / `git-tree.txt`** — repo state at failure time
5. **Read `checkpoint-metadata/`** — corrupt or missing metadata?

## Step 4: Classify Each Failure

### Strong `real-bug` signals (any one is sufficient):

- `entire.log` contains `"level":"ERROR"` or panic/stack traces
- Checkpoint metadata structurally corrupt (malformed JSON, missing `checkpoint_id`/`strategy`)
- Session state file missing or malformed when expected
- Hooks did not fire at all (no `hook invoked` log entries)
- Shadow/metadata branch has wrong tree structure
- Same test fails across 3+ agents with same non-timeout symptom
- Error references CLI code (panic in `cmd/entire/cli/`)

### Strong `flaky` signals (unless overridden by real-bug):

- `signal: killed` (timeout)
- `context deadline exceeded` or `WaitForCheckpoint.*exceeded deadline`
- Agent asked for confirmation instead of acting
- Agent created file at wrong path / wrong name
- Agent produced no output
- Agent committed when it shouldn't have (or vice versa)
- Test fails for only one agent, passes for others
- Duration near timeout limit

### Ambiguous cases:

Read `entire.log` carefully:
- If hooks fired correctly and metadata is valid -> lean **flaky**
- If hooks fired but produced wrong results -> lean **real-bug**

## Step 5: Cross-Agent Correlation

Before acting, check correlations:
- Same test fails for 3+ agents with similar errors -> override to `real-bug`
- Same test fails for only 1 agent -> lean `flaky`

## Step 6: Take Action

### For `flaky` failures: Batched PR

1. Create branch `fix/e2e-flaky-<run-id>`
2. Apply fixes to ALL flaky test files (one branch, one PR):
   - Agent asked for confirmation -> append "Do not ask for confirmation" to prompt
   - Agent wrote to wrong path -> be more explicit about paths in prompt
   - Agent committed when shouldn't -> add "Do not commit" to prompt
   - Checkpoint wait timeout -> increase timeout argument
   - Agent timeout (signal: killed) -> increase per-test timeout, simplify prompt
3. Run verification:
   ```bash
   mise run test:e2e:canary   # Must pass
   mise run fmt && mise run lint
   ```
4. If canary fails, investigate and adjust. If unfixable, fall back to issue creation.
5. Commit and create PR:
   ```bash
   gh pr create \
     --title "fix(e2e): make flaky tests more resilient (run <run-id>)" \
     --body "<structured body with per-test changes, evidence, run link>"
   ```

### For `real-bug` failures: Issue (with dedup)

1. **Search existing issues first:**
   ```bash
   gh issue list --search "is:open label:e2e <TestName>" --json number,title,url
   ```

2. **If matching issue exists:** Add a comment with new evidence:
   ```bash
   gh issue comment <number> --body "<verification details, run link, new evidence>"
   ```
   Note "Verified still failing in CI run <URL>" plus any new diagnostic details.

3. **If no matching issue:** Create new:
   ```bash
   gh issue create \
     --title "E2E: <TestName> fails — <brief symptom>" \
     --label "bug,e2e" \
     --body "<structured body>"
   ```

   Issue body includes:
   - Test name, agent(s), CI run link, frequency
   - Failure summary (expected vs actual)
   - Root cause analysis (which CLI component: hooks, session, checkpoint, attribution, strategy)
   - Key evidence: `entire.log` excerpts, `console.log` excerpts, git state
   - Reproduction steps
   - Suspected fix location (file, function, reason)

## Step 7: Summary Report

Print a summary table:
```
| Test | Agent(s) | Classification | Action | Link |
|------|----------|---------------|--------|------|
| TestFoo | claude-code | flaky | PR #123 | url |
| TestBar | all agents | real-bug | Issue #456 (existing, commented) | url |
| TestBaz | opencode | real-bug | Issue #457 (new) | url |
```

**Write `triage-summary.json`** in the artifact directory for Slack notifications:

```bash
cat > "${ARTIFACT_DIR}/triage-summary.json" << 'TEMPLATE'
{
  "actions": [
    {
      "test": "TestName",
      "agents": ["claude-code", "opencode"],
      "classification": "flaky|real-bug",
      "action_type": "pr|issue|comment",
      "action_description": "PR #123|Issue #456 (new)|Issue #456 (commented)",
      "link": "https://github.com/entireio/cli/pull/123"
    }
  ]
}
TEMPLATE
```

Each entry in `actions` corresponds to one row in the summary table. Include all failures that had an action taken. The `link` field must be the full URL to the PR or issue.
