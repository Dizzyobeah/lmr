---
name: factory
description: Run the software factory. Takes a feature through spec -> build -> parallel reviews -> approval -> pull request, tracking state in factory/board.json via factory/board.py. Project facts, tests and the reviewer set come from factory/config.json. Use for /factory <feature>, /factory next, /factory status, /factory approve <job-id>, /factory rework <job-id> <note>.
argument-hint: <feature> | next | status | approve <job-id> | rework <job-id> <note>
---
# /factory

You are the **orchestrator**. You run the loop below, spawn every agent, and
change `factory/board.json` only through `python3 factory/board.py` (never edit
the JSON by hand). Agents get their context from files and each writes only its
own file.

**Everything project-specific lives in `factory/config.json`**: what the repo is
(`project`), how to test (`tests`), who reviews (`reviewers`), the round limit
(`max_rounds`) and what always needs a human (`escalate_on`). Read it at the start
of every command. To re-target the factory, edit that file or install a profile:
`python3 factory/board.py profile <go-backend|backend|data-engineering|frontend>`.

Paths: `ROOT` = `git rev-parse --show-toplevel` (the main checkout). Give agents
absolute paths built from it.

## The loop (edit this to change the factory)

```
feature
  -> preflight                       tools available? (below)
  -> spec-writer (opus)              writes spec.md (incl. "## Files touched")
  -> overlap check                   refuse to start if it touches files of an in-flight job
  -> builder (opus)                  writes code on factory/<job-id> + build.md
  -> reviewers, in parallel:         one `reviewer` agent per entry in config.reviewers,
                                     each writes round-N/review-<key>.md
  -> WAIT FOR ALL reviews
       any CHANGES and round < max_rounds -> resume builder, next round
  -> approver (opus)                 writes decision.md
       APPROVE  -> open a PR against the latest origin/main (never merge into main)
       ESCALATE -> needs-human (until /factory approve, which also opens a PR)
```

`max_rounds` comes from config and is enforced by `board.py next-round`. If the last
round still has CHANGES, go to the approver anyway; it will ESCALATE.

## Files

```
factory/config.json                  project facts, tests, reviewers, escalation rules
factory/profiles/<name>/config.json  ready-made configs (board.py profile <name>)
factory/board.json                   state, local and git-ignored (board.py creates and writes it)
factory/board.py                     the only way to change state; self-check: factory/test_board.py
factory/backlog.md                   "- [ ] feature" queue; local and git-ignored, created from
                                     factory/backlog.example.md (board.py followups appends)
factory/jobs/<job-id>/spec.md        spec-writer
factory/jobs/<job-id>/build.md       builder
factory/jobs/<job-id>/round-N/review-<key>.md   reviewers
factory/jobs/<job-id>/rework-N.md    you, from /factory rework
factory/jobs/<job-id>/decision.md    approver
factory/jobs/<job-id>/pr-body.md     you, before opening the PR
```

Code lives on branch `factory/<job-id>` in worktree `.claude/worktrees/<job-id>`,
created from `origin/main`. Never switch branches in the main checkout.

## Prompt rules for every agent you spawn

1. **First line of every prompt:** `Write your result to <absolute path> with the Write tool.`
   Then the job id, round, and the paths it needs. Agents have been seen returning
   a report as chat text and skipping the file; this line stops that.
2. Use absolute paths under `ROOT`. Tell agents the test command that preflight found.
3. Never ask a reviewer or the approver to edit files in the job worktree (for example "remove the
   middleware to see the test fail"). Experiments go in a scratch worktree
   `.claude/worktrees/<job-id>-scratch-<who>`; `board.py worktrees` lists leftovers.
4. After an agent finishes, check its file exists. If not, re-spawn it with the same
   prompt plus: "A previous attempt did not write the file. Writing it is your job
   and is explicitly allowed."

## Spawning reviewers

For each entry `r` in `config.reviewers`, spawn subagent type `reviewer` with
model `r.model`, all in **one message** so they run in parallel. Prompt:

```
Write your result to <ROOT>/factory/jobs/<job-id>/round-<N>/review-<r.key>.md with the Write tool.
Job <job-id>, round <N>. Your reviewer key is <r.key>; your checklist is that entry's
`focus` in <ROOT>/factory/config.json. Worktree: <ROOT>/.claude/worktrees/<job-id>.
Test command that works here: <from preflight>.
Never edit the job worktree; for experiments (e.g. a mutation check) use your own scratch
worktree as described in reviewer.md and remove it when done.
```

## Preflight (once per `/factory <feature>`)

Try `config.tests.host` first and `config.tests.docker` second. Run whichever works
on the current `main` with a quick dry run (or `--help`, `version`), and note which
one it was. Also run `git fetch origin`.

If neither works, tell the user before spending agent time: every job would end
`not run` and escalate. If a value still says `TODO`, the config hasn't been
filled in; stop and ask the user to finish `factory/config.json`.

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
4. **Build.** `python3 factory/board.py stage <job-id> build` (sets round 1). Spawn
   `builder` named `builder-<job-id>` ("Job <job-id>. Read spec.md. Round 1."). Then
   check build.md and record its test line:
   `python3 factory/board.py tests <job-id> "<Result line from build.md>"`.
5. **Review round N.** `python3 factory/board.py start-review <job-id>`, then spawn
   the reviewers (above). After they finish, run `python3 factory/board.py sync <job-id>`.
   It prints the verdicts and `round complete` or `round open`. It fails loudly on a
   malformed first line; re-spawn that reviewer.
6. **WAIT FOR ALL.** The round is done only when `sync` prints `round complete`.
   Never resume the builder mid-round.
7. **Follow-ups.** `python3 factory/board.py followups <job-id>`. `[pre-existing]`
   findings go to `backlog.md`. Only `[introduced]` findings block a job.
8. **Decide the round.**
   - Any `CHANGES`: `python3 factory/board.py next-round <job-id>` (it refuses past
     `max_rounds`). Resume `builder-<job-id>` with `SendMessage`: "Round N+1. Read every
     file in factory/jobs/<job-id>/round-N/ and fix the [introduced] CHANGES
     findings. Update build.md." If resuming isn't possible, spawn a new
     `builder-<job-id>` with the same text plus "Read spec.md and build.md first."
     Then go to step 5.
   - Otherwise, or at the round limit: step 9.
9. **Approve.** `python3 factory/board.py stage <job-id> approve`, spawn `approver`,
   then `python3 factory/board.py decide <job-id>`.
   - `APPROVE`: open a PR (below).
   - `ESCALATE`: `decide` already set `needs-human`. Tell the user what decision.md
     asks them to check.

The approver escalates anything matching `config.escalate_on`. A large diff alone is
not a reason to escalate.

## /factory next

Take the first `- [ ]` line in `factory/backlog.md`. Tick it to
`- [x] ... (<job-id>)` when you create the job, then run `/factory <feature>`.
If nothing is unchecked, say the backlog is empty.

## /factory status

Run `python3 factory/board.py status` (refreshes ahead/behind `origin/main`) and
`python3 factory/board.py worktrees`. Then give the user a short summary:
- **In flight:** job, stage, round, verdicts so far.
- **Open PRs:** job and URL. Check each with `gh pr view <url> --json state`, and if
  it's merged run `python3 factory/board.py stage <job-id> merged`.
- **Needs human:** job, and the "For the human" lines from decision.md.
- **Worktrees:** the `remove` lines from `board.py worktrees`. Offer to remove them;
  don't remove without asking.
- **Behind main:** any job branch whose `ahead_behind.behind` is above 0.

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
W=.claude/worktrees/<job-id>
# 1. The job worktree must be clean (tracked files); stop and ask if not.
git -C $W diff --quiet && git -C $W diff --cached --quiet || { echo "dirty worktree"; exit 1; }

# 2. Bring the branch up to the latest origin/main and re-test there.
git -C $W fetch origin
git -C $W merge --no-edit origin/main     # if this conflicts: abort, needs-human
(cd $W && <the test command from preflight>)
# If the tests fail, stop: needs-human, record the failure.

# 3. Publish the branch and open the PR against main.
git -C $W push -u origin factory/<job-id>
gh pr create --base main --head factory/<job-id> --title "<title>" --body-file factory/jobs/<job-id>/pr-body.md
```

`pr-body.md` (you write it before `gh pr create`) holds: the spec summary, each
round's verdicts, the decision, and the test summary line from build.md.
Then `python3 factory/board.py pr <job-id> <PR url>` (sets stage `pr-open`).

Tell the user the PR URL. `stage: "merged"` is set only when the PR is merged on
GitHub, which is the user's action (`/factory status` notices it). Leave the
worktree in place until then.

## Dashboard

`cd factory && python3 -m http.server 8765`, then open
http://localhost:8765/dashboard.html. Mention this the first time you run.
Cards show one verdict chip per configured reviewer and round, the test result,
ahead/behind `origin/main`, and the PR link.
