#!/usr/bin/env python3
"""Factory board helper. Only the orchestrator calls this; agents never do.

Replaces hand-edited board.json. Every write validates the stage, sets `updated`
(UTC) and replaces the file atomically, so the dashboard never reads a half file.

  board.py create <id> <title>          add a job (stage spec), branch/worktree recorded
  board.py files <id>                   read "## Files touched" from spec.md into the job
  board.py overlap <id>                 exit 1 if the job's files overlap an in-flight job
  board.py stage <id> <stage>
  board.py start-review <id>            round dir + all four verdicts pending, stage review
  board.py sync <id>                    read verdict files of the current round
  board.py followups <id>               move [pre-existing] findings of this round to backlog
  board.py next-round <id>              history += current reviews, round+1, stage build
  board.py decide <id>                  read decision.md: APPROVE|ESCALATE (ESCALATE -> needs-human)
  board.py tests <id> "<summary line>"  record the test result shown on the card
  board.py pr <id> <url>                record the PR, stage pr-open
  board.py status                       refresh ahead/behind origin/main for open jobs
  board.py worktrees                    list job worktrees that can be removed
"""
import argparse
import datetime
import json
import os
import re
import subprocess
import sys

HERE = os.environ.get("FACTORY_DIR") or os.path.dirname(os.path.abspath(__file__))
BOARD = os.path.join(HERE, "board.json")
JOBS = os.path.join(HERE, "jobs")
BACKLOG = os.path.join(HERE, "backlog.md")
REPO = os.path.dirname(HERE)

STAGES = ["spec", "build", "review", "approve", "pr-open", "merged", "needs-human"]
IN_FLIGHT = ["spec", "build", "review", "approve", "needs-human"]
REVIEWERS = ["security", "ux", "ui", "code"]
MAX_ROUNDS = 2


def now():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def load():
    with open(BOARD) as f:
        return json.load(f)


def save(board):
    tmp = BOARD + ".tmp"
    with open(tmp, "w") as f:
        json.dump(board, f, indent=2)
        f.write("\n")
    os.replace(tmp, BOARD)


def job_of(board, job_id):
    for j in board["jobs"]:
        if j["id"] == job_id:
            return j
    raise SystemExit(f"no such job: {job_id}")


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
        "worktree": f"../lmr-{a.id}", "stage": "spec", "round": 0,
        "reviews": {}, "history": [], "decision": None,
        "files": [], "tests": None, "ahead_behind": None, "pr": None,
    }
    touch(job)
    board["jobs"].append(job)
    save(board)
    os.makedirs(os.path.join(JOBS, a.id), exist_ok=True)


def cmd_files(a):
    board = load()
    job = job_of(board, a.id)
    spec = os.path.join(JOBS, a.id, "spec.md")
    text = open(spec).read()
    m = re.search(r"^## Files touched\s*\n(.*?)(?=^## |\Z)", text, re.S | re.M)
    if not m:
        raise SystemExit("spec.md has no '## Files touched' section")
    job["files"] = sorted(set(re.findall(r"`([^`\s]+)`", m.group(1))))
    touch(job)
    save(board)
    print("\n".join(job["files"]))


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
    bad = []
    for r in REVIEWERS:
        p = os.path.join(d, f"review-{r}.md")
        if not os.path.exists(p):
            job["reviews"][r] = "pending"
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
    if bad:
        raise SystemExit("malformed verdict(s), re-run those reviewers:\n  " + "\n  ".join(bad))


def cmd_followups(a):
    board = load()
    job = job_of(board, a.id)
    existing = open(BACKLOG).read() if os.path.exists(BACKLOG) else ""
    added = 0
    for r in REVIEWERS:
        p = os.path.join(round_dir(job), f"review-{r}.md")
        if not os.path.exists(p):
            continue
        for text in read_findings(p)["pre-existing"]:
            line = f"- [ ] {text} (found in {a.id}, {r} review)"
            if text and text not in existing:
                with open(BACKLOG, "a") as f:
                    f.write(line + "\n")
                existing += line + "\n"
                added += 1
    print(f"{added} follow-up(s) added to backlog.md")


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


def cmd_status(a):
    git("fetch", "-q", "origin")
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
        ("tests", cmd_tests, ["id", "summary"]),
        ("pr", cmd_pr, ["id", "url"]),
        ("status", cmd_status, []),
        ("worktrees", cmd_worktrees, []),
    ]:
        sp = sub.add_parser(name)
        for arg in args:
            sp.add_argument(arg)
        sp.set_defaults(fn=fn)
    a = p.parse_args(argv)
    a.fn(a)


if __name__ == "__main__":
    main()
