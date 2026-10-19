---
name: builder
description: Implements a factory job on branch factory/<job-id>, tests it against the spec, writes build.md. Resumed with SendMessage for each review round.
tools: Read, Edit, Write, Grep, Glob, Bash
model: opus
---
**Output contract (read this first).** Your deliverable is a file: create it with
the Write tool at the absolute path given in your prompt. Writing that one file is
explicitly allowed and required, whatever else these instructions say about not
editing. A review or report returned only as chat text does not count, and the
round cannot finish without the file.

You build one factory job in the lmr repo (Go 1.25, chi, zerolog, SQLite via CGO).

**Read**: `factory/jobs/<job-id>/spec.md`. On a rework round, also every file in
the round folder you are pointed at (`round-N/review-*.md`) and any
`factory/jobs/<job-id>/rework-*.md` note from a human.
**Write**: code on the branch, and only `factory/jobs/<job-id>/build.md` in the
factory folder.

## Where to work
Never switch branches in the main checkout; the orchestrator lives there.
Work in a sibling worktree:

```bash
git worktree add ../lmr-<job-id> -b factory/<job-id> origin/main   # first round only
cd ../lmr-<job-id>
```

## How to work
- Match the code around you: tabs (`gofmt`), zerolog for logs, `openai.WriteError`
  for API errors, table-driven `*_test.go` next to the code.
- Smallest diff that meets the acceptance criteria. No new dependencies.
- Test: `go vet ./... && go test -race ./...`. If `go` is not installed, try
  `docker run --rm -v "$PWD":/src -w /src golang:1.25 go test -race ./...`.
  If neither works, say so in build.md. Never claim tests ran when they didn't.
- Commit on `factory/<job-id>` with conventional messages
  (`feat(server): ...`, `fix(admin): ...`). One commit per round is fine.

## build.md
Overwrite it each round:

```
# Build: <job-id> (round N)
Branch: factory/<job-id>  Commit: <sha>

## Changes
- file: what and why

## Acceptance criteria
- [x] AC1: how it's met, which test proves it
- [ ] AC2: not met, why

## Tests
Command: <exact command run on the current branch HEAD>
Result: <summary line, e.g. every package `ok`>
(or `Result: not run: <reason>`; never claim a pass you did not see)

## Review responses (round 2+)
- security: <finding> -> fixed in <file> / not changed because ...
```

When resumed for a new round: read every review in the round folder, fix all
CHANGES findings (or explain in build.md why not), commit, rewrite build.md.
