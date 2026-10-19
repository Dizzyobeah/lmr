#!/usr/bin/env python3
"""Self-check for board.py: `python3 factory/test_board.py` (no framework)."""
import contextlib
import io
import json
import os
import sys
import tempfile

tmp = tempfile.mkdtemp()
os.environ["FACTORY_DIR"] = tmp
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import board  # noqa: E402

open(os.path.join(tmp, "board.json"), "w").write('{"jobs": []}')
open(os.path.join(tmp, "backlog.md"), "w").write("# backlog\n")


def run(*argv):
    out = io.StringIO()
    with contextlib.redirect_stdout(out):
        try:
            board.main(list(argv))
        except SystemExit as e:
            if e.code not in (None, 0):  # exit(0) is a normal result (e.g. no overlap)
                raise
    return out.getvalue()


def job(i):
    return [j for j in json.load(open(board.BOARD))["jobs"] if j["id"] == i][0]


def write(jid, rel, text):
    path = os.path.join(tmp, "jobs", jid, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    open(path, "w").write(text)


# verdict parsing: tolerant of decoration, loud on anything else
assert board.parse_verdict("VERDICT: PASS\n", ["PASS", "CHANGES"]) == "PASS"
assert board.parse_verdict("**Verdict: changes**", ["PASS", "CHANGES"]) == "CHANGES"
assert board.parse_verdict("APPROVE", ["APPROVE", "ESCALATE"]) == "APPROVE"
for bad in ["I think this passes", "", "VERDICT: MAYBE", "# Review"]:
    try:
        board.parse_verdict(bad, ["PASS", "CHANGES"])
        raise AssertionError(f"accepted {bad!r}")
    except ValueError:
        pass

# lifecycle
run("create", "001-a", "Job A")
write("001-a", "spec.md", "# s\n## Files touched\n- `internal/x.go`\n- `README.md`\n## Notes\n`ignored.go`\n")
assert run("files", "001-a").split() == ["README.md", "internal/x.go"]
run("create", "002-b", "Job B")
write("002-b", "spec.md", "## Files touched\n- `internal/x.go`\n")
run("files", "002-b")
try:
    run("overlap", "002-b")
    raise AssertionError("overlap not detected")
except SystemExit as e:
    assert e.code == 1
run("stage", "001-a", "pr-open")  # no longer in flight
run("overlap", "002-b")

run("stage", "002-b", "build")
assert job("002-b")["round"] == 1  # first build is round 1
run("start-review", "002-b")
assert job("002-b")["reviews"] == {r: "pending" for r in board.REVIEWERS}

# sync: one malformed file is reported and stays pending
write("002-b", "round-1/review-security.md", "VERDICT: PASS\n")
write("002-b", "round-1/review-ux.md", "VERDICT: CHANGES\n- [introduced] bad label (ux.go:3)\n- [pre-existing] old bug (x.go:9)\n")
write("002-b", "round-1/review-ui.md", "Looks fine to me\n")
try:
    run("sync", "002-b")
    raise AssertionError("malformed verdict not rejected")
except SystemExit as e:
    assert "ui:" in str(e)
assert job("002-b")["reviews"]["security"] == "PASS"
assert job("002-b")["reviews"]["ui"] == "pending"
write("002-b", "round-1/review-ui.md", "VERDICT: PASS\n")
write("002-b", "round-1/review-code.md", "VERDICT: PASS\n- [pre-existing] old bug (x.go:9)\n")
assert "round complete" in run("sync", "002-b")

# follow-ups: pre-existing findings go to the backlog once
run("followups", "002-b")
run("followups", "002-b")
backlog = open(board.BACKLOG).read()
assert backlog.count("old bug (x.go:9)") == 1 and "found in 002-b" in backlog

# rounds
run("next-round", "002-b")
j = job("002-b")
assert j["round"] == 2 and j["stage"] == "build" and len(j["history"]) == 1
run("start-review", "002-b")
try:
    run("next-round", "002-b")
    raise AssertionError("round limit not enforced")
except SystemExit as e:
    assert "max" in str(e)

# decision
write("002-b", "decision.md", "ESCALATE\n\n## Why\n- auth change\n")
assert run("decide", "002-b").strip() == "ESCALATE"
assert job("002-b")["stage"] == "needs-human"

# tests line, PR
run("tests", "002-b", "ok  internal/admin 1.2s")
assert job("002-b")["tests"].startswith("ok")
run("pr", "002-b", "https://github.com/o/r/pull/9")
assert job("002-b")["stage"] == "pr-open" and job("002-b")["pr"].endswith("/9")
try:
    run("stage", "002-b", "bogus")
    raise AssertionError("bad stage accepted")
except SystemExit:
    pass
assert job("002-b")["updated"].endswith("Z")
print("ok: board.py self-check passed")
