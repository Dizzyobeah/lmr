#!/usr/bin/env python3
"""Factory board helper. Only the orchestrator calls this; agents never do.

Replaces hand-edited board.json. Every write validates the stage, sets `updated`
(UTC) and replaces the file atomically, so the dashboard never reads a half file.

  board.py create <id> <title>          add a job (stage spec), branch/worktree recorded
  board.py files <id>                   read "## Files touched" from spec.md into the job
  board.py overlap <id>                 exit 1 if the job's files overlap an in-flight job
  board.py stage <id> <stage>
  board.py start-review <id>            round dir + all four verdicts pending, stage review
  board.py sync <id>                    read verdict files of the current round (names missing files)
  board.py incident <id> <agent> <note> log a miss (agent that did not write its file) in jobs/<id>/incidents.md
  board.py followups <id>               move [pre-existing] findings of this round to backlog
  board.py next-round <id>              history += current reviews, round+1, stage build
  board.py decide <id>                  read decision.md: APPROVE|ESCALATE (ESCALATE -> needs-human)
  board.py tests <id> "<summary line>"  record the test result shown on the card
  board.py pr <id> <url>                record the PR, stage pr-open
  board.py status                       refresh ahead/behind origin/main for open jobs, then sync-prs
  board.py sync-prs                     mark pr-open jobs whose PR is merged on GitHub as merged (needs gh)
  board.py serve [--port N] [--interval S]  serve the dashboard and run sync-prs every S seconds
  board.py worktrees                    list job worktrees that can be removed
  board.py profile <name> [--force]     install factory/profiles/<name>/config.json as factory/config.json

Reviewers and the round limit come from factory/config.json (`reviewers`, `max_rounds`).
"""
import argparse
import datetime
import filecmp
import functools
import http.server
import json
import os
import re
import shutil
import subprocess
import sys
import threading
import time

HERE = os.environ.get("FACTORY_DIR") or os.path.dirname(os.path.abspath(__file__))
BOARD = os.path.join(HERE, "board.json")
JOBS = os.path.join(HERE, "jobs")
BACKLOG = os.path.join(HERE, "backlog.md")
CONFIG = os.path.join(HERE, "config.json")
PROFILES = os.path.join(HERE, "profiles")
REPO = os.path.dirname(HERE)

STAGES = ["spec", "build", "review", "approve", "pr-open", "merged", "needs-human"]
IN_FLIGHT = ["spec", "build", "review", "approve", "needs-human"]
DEFAULT_REVIEWERS = [["security", "sec"], ["ux", "ux"], ["ui", "ui"], ["code", "code"]]


def load_config():
    """Reviewers ([key, label] pairs) and max rounds from config.json, with defaults."""
    try:
        with open(CONFIG) as f:
            cfg = json.load(f)
    except FileNotFoundError:
        return DEFAULT_REVIEWERS, 2
    revs = [[r["key"], r.get("label", r["key"])] for r in cfg.get("reviewers", [])]
    keys = [k for k, _ in revs]
    if not revs or len(set(keys)) != len(keys):
        raise SystemExit("config.json: `reviewers` must be a non-empty list with unique keys")
    return revs, int(cfg.get("max_rounds", 2))


REVIEWER_LABELS, MAX_ROUNDS = load_config()
REVIEWERS = [k for k, _ in REVIEWER_LABELS]


def now():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def load():
    """board.json is local state (git-ignored); start empty if it doesn't exist."""
    try:
        with open(BOARD) as f:
            return json.load(f)
    except FileNotFoundError:
        return {"jobs": []}


def save(board):
    board["reviewers"] = REVIEWER_LABELS  # the dashboard draws its chips from this
    tmp = f"{BOARD}.{os.getpid()}.tmp"  # per process: the serve thread and a command may save together
    with open(tmp, "w") as f:
        json.dump(board, f, indent=2)
        f.write("\n")
    os.replace(tmp, BOARD)


def job_of(board, job_id):
    for j in board["jobs"]:
        if j["id"] == job_id:
            return j
    raise SystemExit(f"no such job: {job_id}")


def ensure_backlog():
    """backlog.md is local state (git-ignored), like board.json: create it when it is missing."""
    if os.path.exists(BACKLOG):
        return
    example = os.path.join(HERE, "backlog.example.md")
    if os.path.exists(example):
        shutil.copyfile(example, BACKLOG)
    else:
        with open(BACKLOG, "w") as f:
            f.write("# Factory backlog\n\n`/factory next` runs the first unchecked item.\n\n")


def touch(job):
    job["updated"] = now()


def first_line(path):
    with open(path) as f:
        return f.readline()


def parse_verdict(line, words):
    """First line must be one of `words`, optionally as `VERDICT: <word>`.
    Tolerates markdown decoration (**, `, #) and case; anything else is an error."""
    text = re.sub(r"[*`#_>\s]+", " ", line).strip().rstrip(".:").strip()
    text = re.sub(r"^(?i:verdict)\s*:?\s*", "", text).strip().upper()
    if text not in words:
        raise ValueError(f"first line must be {' or '.join(words)}, got {line.strip()!r}")
    return text


def round_dir(job, n=None):
    return os.path.join(JOBS, job["id"], f"round-{n or job['round']}")


def read_findings(path):
    """Finding lines look like `- [introduced] ...` or `- [pre-existing] ...`."""
    out = {"introduced": [], "pre-existing": []}
    with open(path) as f:
        for line in f:
            m = re.match(r"^\s*[-*]\s*\[(introduced|pre-existing)\]\s*(.*)$", line, re.I)
            if m:
                out[m.group(1).lower()].append(m.group(2).strip())
    return out


# ---- commands ----------------------------------------------------------------

def cmd_create(a):
    board = load()
    if any(j["id"] == a.id for j in board["jobs"]):
        raise SystemExit(f"job exists: {a.id}")
    job = {
        "id": a.id, "title": a.title, "branch": f"factory/{a.id}",
        "worktree": f".claude/worktrees/{a.id}", "stage": "spec", "round": 0,
        "reviews": {}, "history": [], "decision": None,
        "files": [], "tests": None, "ahead_behind": None, "pr": None,
    }
    touch(job)
    board["jobs"].append(job)
    save(board)
    os.makedirs(os.path.join(JOBS, a.id), exist_ok=True)
    ensure_backlog()


# Extensions that mark a token as a file. A short allowlist, because a config key such as
# `server.port` also has a dot and a short tail but is not a path.
FILE_EXTS = {"go", "md", "yaml", "yml", "json", "toml", "html", "css", "js", "ts", "tsx", "jsx", "py",
             "sql", "sh", "txt", "mod", "sum", "proto", "xml", "csv", "env", "ini", "cfg", "conf",
             "lock", "rs", "java", "kt", "rb", "tf", "gradle", "cs", "php", "swift", "c", "h", "cpp"}
BARE_FILES = {"Makefile", "Dockerfile", "LICENSE", "Jenkinsfile"}


def looks_like_path(tok):
    if "/" in tok or tok in BARE_FILES:
        return True
    return "." in tok and tok.rsplit(".", 1)[1].lower() in FILE_EXTS


def parse_files(text):
    """Paths from the '## Files touched' section of a spec.

    Only the backticked tokens that LEAD a bullet count (`- `path`: what changes`); anything after
    the colon is prose, so identifiers and config keys there are ignored. A leading token that is
    not path-like is dropped and reported in the returned warnings."""
    m = re.search(r"^## Files touched[ \t]*\n(.*?)(?=^## |\Z)", text, re.S | re.M)
    if not m:
        raise SystemExit("spec.md has no '## Files touched' section")
    paths, warnings = set(), []
    for line in m.group(1).splitlines():
        lead = re.match(r"^\s*[-*]\s+((?:`[^`\s]+`[ \t]*,?[ \t]*)+)", line)
        if not lead:
            if re.match(r"^\s*[-*]\s", line):
                warnings.append(f"bullet does not start with a `path`: {line.strip()[:70]}")
            continue
        for tok in re.findall(r"`([^`\s]+)`", lead.group(1)):
            if looks_like_path(tok):
                paths.add(tok)
            else:
                warnings.append(f"not a file path, ignored: {tok}")
    return sorted(paths), warnings


def cmd_files(a):
    board = load()
    job = job_of(board, a.id)
    with open(os.path.join(JOBS, a.id, "spec.md")) as f:
        paths, warnings = parse_files(f.read())
    for w in warnings:
        print(f"warning: {w}", file=sys.stderr)
    if not paths:
        raise SystemExit("spec.md lists no file paths under '## Files touched'")
    job["files"] = paths
    touch(job)
    save(board)
    print("\n".join(paths))


def cmd_overlap(a):
    board = load()
    job = job_of(board, a.id)
    mine = set(job.get("files") or [])
    clash = []
    for other in board["jobs"]:
        if other["id"] != a.id and other["stage"] in IN_FLIGHT:
            both = mine & set(other.get("files") or [])
            if both:
                clash.append((other["id"], sorted(both)))
    for oid, both in clash:
        print(f"overlaps {oid}: {', '.join(both)}")
    sys.exit(1 if clash else 0)


def cmd_stage(a):
    if a.stage not in STAGES:
        raise SystemExit(f"stage must be one of {STAGES}")
    board = load()
    job = job_of(board, a.id)
    job["stage"] = a.stage
    if a.stage == "build" and job["round"] == 0:
        job["round"] = 1  # the first build is round 1
    touch(job)
    save(board)


def cmd_start_review(a):
    board = load()
    job = job_of(board, a.id)
    if job["round"] < 1:
        raise SystemExit("round is 0: build first")
    os.makedirs(round_dir(job), exist_ok=True)
    job["stage"] = "review"
    job["reviews"] = {r: "pending" for r in REVIEWERS}
    touch(job)
    save(board)


def cmd_sync(a):
    board = load()
    job = job_of(board, a.id)
    d = round_dir(job)
    bad, missing = [], []
    for r in REVIEWERS:
        p = os.path.join(d, f"review-{r}.md")
        if not os.path.exists(p):
            job["reviews"][r] = "pending"
            missing.append(r)
            continue
        try:
            job["reviews"][r] = parse_verdict(first_line(p), ["PASS", "CHANGES"])
        except ValueError as e:
            bad.append(f"{r}: {e}")
            job["reviews"][r] = "pending"
            continue
        if job["reviews"][r] == "CHANGES" and not read_findings(p)["introduced"]:
            print(f"warning: {r} says CHANGES with no [introduced] finding", file=sys.stderr)
    touch(job)
    save(board)
    print(json.dumps(job["reviews"]))
    done = all(v in ("PASS", "CHANGES") for v in job["reviews"].values())
    print("round complete" if done else "round open")
    if missing:
        print("missing: " + ", ".join(missing))
    if bad:
        raise SystemExit("malformed verdict(s), re-run those reviewers:\n  " + "\n  ".join(bad))


LOCATION = re.compile(r"([\w./-]+):(\d+)")


def location_key(text):
    """(path, first line) of the first `path:line` in a finding, or None."""
    for m in LOCATION.finditer(text):
        if looks_like_path(m.group(1)):
            return (m.group(1), int(m.group(2)))
    return None


def cmd_followups(a):
    """Queue [pre-existing] findings of this round in the backlog.

    A finding needs a `path:line`. Two findings with the same path and first line are the same
    problem even if worded differently, so only the first is queued (also across earlier runs)."""
    board = load()
    job = job_of(board, a.id)
    ensure_backlog()
    existing = open(BACKLOG).read()
    seen = {k for k in map(location_key, existing.splitlines()) if k}
    added = dupes = 0
    for r in REVIEWERS:
        p = os.path.join(round_dir(job), f"review-{r}.md")
        if not os.path.exists(p):
            continue
        for text in read_findings(p)["pre-existing"]:
            key = location_key(text)
            if key is None:
                print(f"warning: {r} review has a [pre-existing] finding with no path:line, not queued: "
                      f"{text[:80]}", file=sys.stderr)
                continue
            if key in seen:
                dupes += 1
                continue
            seen.add(key)
            with open(BACKLOG, "a") as f:
                f.write(f"- [ ] {text} (found in {a.id}, {r} review)\n")
            added += 1
    print(f"{added} follow-up(s) added to backlog.md, {dupes} duplicate(s) skipped")


def cmd_next_round(a):
    board = load()
    job = job_of(board, a.id)
    if job["round"] >= MAX_ROUNDS:
        raise SystemExit(f"already at round {job['round']} (max {MAX_ROUNDS}): go to the approver")
    job["history"].append(dict(job["reviews"]))
    job.update(round=job["round"] + 1, stage="build", reviews={}, decision=None)
    touch(job)
    save(board)


def cmd_decide(a):
    board = load()
    job = job_of(board, a.id)
    p = os.path.join(JOBS, a.id, "decision.md")
    job["decision"] = parse_verdict(first_line(p), ["APPROVE", "ESCALATE"])
    if job["decision"] == "ESCALATE":
        job["stage"] = "needs-human"
    touch(job)
    save(board)
    print(job["decision"])


def cmd_incident(a):
    """Append a dated line to jobs/<id>/incidents.md (an agent that finished without its file)."""
    job_of(load(), a.id)  # refuses an unknown job
    path = os.path.join(JOBS, a.id, "incidents.md")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    new = not os.path.exists(path)
    with open(path, "a") as f:
        if new:
            f.write(f"# Incidents: {a.id}\n\n")
        f.write(f"- {now()} {a.agent}: {a.note}\n")
    print(path)


def cmd_tests(a):
    board = load()
    job = job_of(board, a.id)
    job["tests"] = a.summary
    touch(job)
    save(board)


def cmd_pr(a):
    board = load()
    job = job_of(board, a.id)
    job.update(pr=a.url, stage="pr-open")
    touch(job)
    save(board)


def git(*args):
    return subprocess.run(["git", "-C", REPO, *args], capture_output=True, text=True)


def pr_state(url):
    """OPEN, MERGED or CLOSED from GitHub, or None when gh can't tell (missing, offline, not logged in)."""
    try:
        r = subprocess.run(["gh", "pr", "view", url, "--json", "state", "-q", ".state"],
                           capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.SubprocessError):
        return None
    state = r.stdout.strip()
    return state if r.returncode == 0 and state in ("OPEN", "MERGED", "CLOSED") else None


def sync_prs():
    """Mark pr-open jobs whose PR merged as merged. Returns (merged ids, closed-unmerged ids).

    Ask GitHub first, then reload and apply: the slow gh calls never hold a stale copy of the
    board, so a command saving in the meantime is not overwritten.
    """
    # ponytail: reload-then-save is not locked; a lost update needs a save inside those few ms.
    # Upgrade to a lock file if it ever happens.
    states = {j["id"]: pr_state(j["pr"]) for j in load()["jobs"] if j["stage"] == "pr-open" and j.get("pr")}
    board = load()
    merged, closed = [], []
    for job in board["jobs"]:
        state = states.get(job["id"])
        if job["stage"] != "pr-open" or state is None:
            continue
        if state == "MERGED":
            job.update(stage="merged", merged_at=now())
            touch(job)
            merged.append(job["id"])
        elif state == "CLOSED":
            closed.append(job["id"])
    if merged:
        save(board)
    return merged, closed


def cmd_sync_prs(a):
    merged, closed = sync_prs()
    for jid in merged:
        print(f"merged: {jid}")
    for jid in closed:
        print(f"closed-unmerged: {jid}")
    if not merged and not closed:
        print("no PR changes")


def cmd_serve(a):
    """The dashboard plus a loop that keeps merged PRs current, so it needs no /factory command."""
    def loop():
        while True:
            try:
                sync_prs()
            except Exception as e:  # keep serving; the next tick retries
                print(f"sync-prs failed: {e}", file=sys.stderr)
            time.sleep(a.interval)

    threading.Thread(target=loop, daemon=True).start()
    handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=HERE)
    server = http.server.ThreadingHTTPServer(("127.0.0.1", a.port), handler)
    print(f"http://localhost:{a.port}/dashboard.html  (merged PRs checked every {a.interval}s)")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


def cmd_status(a):
    git("fetch", "-q", "origin")
    sync_prs()
    board = load()
    for job in board["jobs"]:
        if job["stage"] == "merged":
            continue
        r = git("rev-list", "--left-right", "--count", f"origin/main...{job['branch']}")
        if r.returncode == 0:
            behind, ahead = r.stdout.split()
            job["ahead_behind"] = {"ahead": int(ahead), "behind": int(behind)}
            touch(job)
    save(board)
    for job in board["jobs"]:
        print(job["id"], job["stage"], job.get("ahead_behind"))


def cmd_worktrees(a):
    board = load()
    for job in board["jobs"]:
        path = os.path.normpath(os.path.join(REPO, job.get("worktree") or ""))
        if not job.get("worktree") or not os.path.isdir(path):
            continue
        if job["stage"] == "merged":
            print(f"remove  {job['id']}  git worktree remove {job['worktree']}")
        else:
            print(f"keep    {job['id']}  ({job['stage']})  {job['worktree']}")
    # Scratch worktrees are private copies a reviewer makes for experiments; none should outlive it.
    root = os.path.join(REPO, ".claude", "worktrees")
    if os.path.isdir(root):
        for name in sorted(os.listdir(root)):
            if re.search(r"-scratch-.+$", name) and os.path.isdir(os.path.join(root, name)):
                print(f"stale-scratch  {name}  git worktree remove --force .claude/worktrees/{name}")


def cmd_profile(a):
    src = os.path.join(PROFILES, a.name, "config.json")
    if not os.path.exists(src):
        names = sorted(os.listdir(PROFILES)) if os.path.isdir(PROFILES) else []
        raise SystemExit(f"no profile {a.name!r}; available: {', '.join(names) or 'none'}")
    if os.path.exists(CONFIG) and not filecmp.cmp(src, CONFIG, shallow=False) and not a.force:
        raise SystemExit("factory/config.json differs from the profile; re-run with --force to replace it")
    in_flight = [j["id"] for j in load()["jobs"] if j["stage"] in IN_FLIGHT] if os.path.exists(BOARD) else []
    if in_flight and not a.force:
        raise SystemExit(f"jobs in flight ({', '.join(in_flight)}); finish them or use --force")
    shutil.copyfile(src, CONFIG)
    print(f"installed profile {a.name} -> factory/config.json")
    print("now edit factory/config.json: project, tests and reviewer focus for this repo")


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    for name, fn, args in [
        ("create", cmd_create, ["id", "title"]),
        ("files", cmd_files, ["id"]),
        ("overlap", cmd_overlap, ["id"]),
        ("stage", cmd_stage, ["id", "stage"]),
        ("start-review", cmd_start_review, ["id"]),
        ("sync", cmd_sync, ["id"]),
        ("followups", cmd_followups, ["id"]),
        ("next-round", cmd_next_round, ["id"]),
        ("decide", cmd_decide, ["id"]),
        ("incident", cmd_incident, ["id", "agent", "note"]),
        ("tests", cmd_tests, ["id", "summary"]),
        ("pr", cmd_pr, ["id", "url"]),
        ("status", cmd_status, []),
        ("sync-prs", cmd_sync_prs, []),
        ("serve", cmd_serve, []),
        ("worktrees", cmd_worktrees, []),
        ("profile", cmd_profile, ["name"]),
    ]:
        sp = sub.add_parser(name)
        for arg in args:
            sp.add_argument(arg)
        if name == "profile":
            sp.add_argument("--force", action="store_true")
        if name == "serve":
            sp.add_argument("--port", type=int, default=8765)
            sp.add_argument("--interval", type=int, default=30)
        sp.set_defaults(fn=fn)
    a = p.parse_args(argv)
    a.fn(a)


if __name__ == "__main__":
    main()
