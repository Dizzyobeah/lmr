---
name: factory
description: Run the software factory. Takes a feature through spec -> build -> 4 parallel reviews -> approval -> pull request, tracking state in factory/board.json via factory/board.py. Use for /factory <feature>, /factory next, /factory approve <job-id>, /factory rework <job-id> <note>.
argument-hint: <feature> | next | approve <job-id> | rework <job-id> <note>
---
# /factory

You are the **orchestrator**. You run the loop below, spawn every agent, and
change `factory/board.json` only through `python3 factory/board.py` (never edit
the JSON by hand). Agents get their context from files and each writes only its
own file.

## The loop (edit this to change the factory)

```
feature
  -> preflight                       tools available? (below)
  -> spec-writer (opus)              writes spec.md (incl. "## Files touched")
  -> overlap check                   refuse to start if it touches files of an in-flight job
  -> builder (opus)                  writes code on factory/<job-id> + build.md
  -> reviewers, in parallel:         write round-N/review-<r>.md
       reviewer-security (opus)  reviewer-ux (sonnet)
       reviewer-ui (sonnet)      reviewer-code (sonnet)
  -> WAIT FOR ALL 4 reviews
       any CHANGES and round < MAX_ROUNDS -> resume builder, next round
  -> approver (opus)                 writes decision.md
       APPROVE  -> open a PR against the latest origin/main (never merge into main)
       ESCALATE -> needs-human (until /factory approve, which also opens a PR)
```

`MAX_ROUNDS = 2` (enforced by `board.py next-round`). If round 2 still has
CHANGES, go to the approver anyway; it will ESCALATE.

Reviewers and their file names:

| reviewer key | agent | file |
|---|---|---|
| security | reviewer-security | review-security.md |
| ux | reviewer-ux | review-ux.md |
| ui | reviewer-ui | review-ui.md |
| code | reviewer-code | review-code.md |

## Files

```
factory/board.json                   state (board.py writes it)
factory/board.py                     the only way to change state; self-check: factory/test_board.py
factory/backlog.md                   "- [ ] feature" queue (board.py followups appends)
factory/jobs/<job-id>/spec.md        spec-writer
factory/jobs/<job-id>/build.md       builder
factory/jobs/<job-id>/round-N/review-<r>.md   reviewers
factory/jobs/<job-id>/rework-N.md    you, from /factory rework
factory/jobs/<job-id>/decision.md    approver
factory/jobs/<job-id>/pr-body.md     you, before opening the PR
```

Code lives on branch `factory/<job-id>` in worktree `.claude/worktrees/<job-id>`, created
from `origin/main`. Never switch branches in this checkout.

## board.json

Managed by `board.py`; the dashboard polls it. Shape of a job:

```json
{ "id": "003-rate-limiting", "title": "Rate limit per API key",
  "branch": "factory/003-rate-limiting", "worktree": ".claude/worktrees/003-rate-limiting",
  "stage": "review", "round": 2,
  "reviews": { "security": "CHANGES", "ux": "PASS", "ui": "pending", "code": "PASS" },
  "history": [ { "security": "CHANGES", "ux": "PASS", "ui": "PASS", "code": "CHANGES" } ],
  "decision": null, "files": ["internal/middleware/ratelimit.go"],
  "tests": "ok internal/middleware 1.2s", "ahead_behind": { "ahead": 1, "behind": 0 },
  "pr": null, "updated": "2026-10-16T10:00:00Z" }
```

- `stage`: `spec` | `build` | `review` | `approve` | `pr-open` | `merged` | `needs-human`
  (`pr-open`: PR is open against main; `merged`: the PR was merged on GitHub)
- `reviews`: the current round; each value `pending` | `PASS` | `CHANGES`
- Run `python3 factory/board.py status` now and then: it refreshes `ahead_behind`.

## Prompt rules for every agent you spawn

1. **First line of every prompt:** `Write your result to <absolute path> with the Write tool.`
   Then the job id, round, and the other paths it needs. Agents have been seen
   returning a report as chat text and skipping the file; the first line stops that.
2. Give absolute paths (main checkout `/Users/calund/projects/_git/lmr`, worktree
   `.claude/worktrees/<job-id>`).
3. After the agent finishes, check the file exists. If not, re-spawn that agent
   with the same prompt plus: "A previous attempt did not write the file. Writing it
   is your job and is explicitly allowed."

## Preflight (once per `/factory <feature>`)

```bash
command -v go >/dev/null && echo "go: host" || { docker info >/dev/null 2>&1 && echo "go: docker (golang:1.25)" || echo "go: UNAVAILABLE"; }
git fetch origin
```

Tell agents which test command works: `go test -race ./...` on the host, or
`docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c 'go vet ./... && go test -race ./...'`.
If neither works, say so to the user before spending agent time: every job would
end `not run` and escalate.

## /factory <feature>

1. **Create the job.** Id = next 3-digit number after the highest existing id, plus
   a 2-4 word kebab slug. `python3 factory/board.py create <job-id> "<title>"`.
2. **Spec.** Spawn `spec-writer`. Check spec.md exists, then
   `python3 factory/board.py files <job-id>` (reads "## Files touched").
3. **Overlap check.** `python3 factory/board.py overlap <job-id>`. If it exits 1,
   another in-flight job touches the same files: tell the user, leave this job at
   stage `spec`, and don't start the builder until the other job's PR is merged.
   If the spec proposes stages (`## Later stages`), run only the first now and add
   the later stages to `factory/backlog.md`.
4. **Build.** `python3 factory/board.py stage <job-id> build` and set round 1 (the
   builder prompt says "Round 1"). Spawn `builder` named `builder-<job-id>`.
   After it finishes: check build.md, and record its test line with
   `python3 factory/board.py tests <job-id> "<Result line from build.md>"`.
5. **Review round N.** `python3 factory/board.py start-review <job-id>`. Spawn all
   four reviewers **in one message**. After each finishes (or after all), run
   `python3 factory/board.py sync <job-id>`; it prints the verdicts and
   `round complete` or `round open`, and fails loudly on a malformed first line
   (re-spawn that reviewer).
6. **WAIT FOR ALL.** The round is done only when `sync` prints `round complete`.
   Never resume the builder mid-round.
7. **Follow-ups.** When the round is complete, run
   `python3 factory/board.py followups <job-id>`: `[pre-existing]` findings go to
   `backlog.md` automatically. Only `[introduced]` findings block a job.
8. **Decide the round.**
   - Any `CHANGES`: `python3 factory/board.py next-round <job-id>` (it refuses past
     MAX_ROUNDS). Resume `builder-<job-id>` with `SendMessage`: "Round N+1. Read every
     file in factory/jobs/<job-id>/round-N/ and fix the [introduced] CHANGES
     findings. Update build.md." (If resuming isn't possible, spawn a new
     `builder-<job-id>` with the same text plus "Read spec.md and build.md first.")
     Then go to step 5.
   - Otherwise: step 9. (At the round limit with CHANGES still open, also go to step 9.)
9. **Approve.** `python3 factory/board.py stage <job-id> approve`, spawn `approver`,
   then `python3 factory/board.py decide <job-id>`.
   - `APPROVE`: open a PR (below).
   - `ESCALATE`: `decide` already set `needs-human`. Tell the user what decision.md
     asks them to check.

### Escalation rules (the approver applies these; keep them in sync with approver.md)

Always escalate: a change to the auth middleware or its allow-list (including any
new unauthenticated route), crypto or secret handling, a new public route, a new
external resource load (script, font, CDN), a new dependency, a breaking API or
config change, a data migration, or missing/`not run` test evidence. A large diff
alone is not a reason to escalate.

## /factory next

Take the first `- [ ]` line in `factory/backlog.md`. Tick it to
`- [x] ... (<job-id>)` when you create the job, then run `/factory <feature>`.
If nothing is unchecked, say the backlog is empty.

## /factory approve <job-id>

Only for `needs-human` jobs. Append `Approved by human on <date>.` to decision.md,
then open a PR (below). It does not merge locally.

## /factory rework <job-id> <note>

Write the note to `factory/jobs/<job-id>/rework-<N>.md` where N is the current
round, then `python3 factory/board.py next-round <job-id>`. Resume
`builder-<job-id>` (step 8 wording, plus "Also read rework-<N>.md"), then continue
from step 5.

## Open a PR (never merge into main)

Approved work is delivered as a pull request based on the latest `origin/main`.
Nothing in this skill merges into `main`, locally or on GitHub.

```bash
# 1. The job worktree must be clean (tracked files); stop and ask if not.
git -C .claude/worktrees/<job-id> diff --quiet && git -C .claude/worktrees/<job-id> diff --cached --quiet || { echo "dirty worktree"; exit 1; }

# 2. Bring the branch up to the latest origin/main and re-test there.
git -C .claude/worktrees/<job-id> fetch origin
git -C .claude/worktrees/<job-id> merge --no-edit origin/main   # if this conflicts: abort, needs-human
docker run --rm -v "$PWD/.claude/worktrees/<job-id>":/src -w /src golang:1.25 sh -c 'go vet ./... && go test -race ./...'
# If the tests fail, stop: needs-human, record the failure.

# 3. Publish the branch and open the PR against main.
git -C .claude/worktrees/<job-id> push -u origin factory/<job-id>
gh pr create --base main --head factory/<job-id> --title "<title>" --body-file factory/jobs/<job-id>/pr-body.md
```

`pr-body.md` (you write it before `gh pr create`) holds: the spec summary, each
round's verdicts, the decision, and the test summary line from build.md.
Then `python3 factory/board.py pr <job-id> <PR url>` (sets stage `pr-open`).

Tell the user the PR URL. `stage: "merged"` is set only when the PR is merged on
GitHub (`python3 factory/board.py stage <job-id> merged`), which is the user's
action. Leave the worktree in place until then.

## Cleanup

`python3 factory/board.py worktrees` lists job worktrees: `remove` for merged jobs
(run the printed `git worktree remove` command), `keep` for the rest. Escalated
(`needs-human`) jobs keep their worktree on purpose; its path is in the job's
`worktree` field.

## Dashboard

`cd factory && python3 -m http.server 8765`, then open
http://localhost:8765/dashboard.html. Mention this the first time you run.
Cards show verdict chips per round, the test result line, ahead/behind
`origin/main`, and the PR link.
