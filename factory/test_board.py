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
run("tests", "002-b", "ok  all packages 1.2s")
assert job("002-b")["tests"].startswith("ok")
run("pr", "002-b", "https://github.com/o/r/pull/9")
assert job("002-b")["stage"] == "pr-open" and job("002-b")["pr"].endswith("/9")
try:
    run("stage", "002-b", "bogus")
    raise AssertionError("bad stage accepted")
except SystemExit:
    pass
assert job("002-b")["updated"].endswith("Z")
# board.json carries the reviewer list for the dashboard
assert json.load(open(board.BOARD))["reviewers"] == board.DEFAULT_REVIEWERS

# ---- custom reviewer set from config.json (nothing hard-coded) ----------------
import subprocess  # noqa: E402

tmp2 = tempfile.mkdtemp()
cfg = {"reviewers": [{"key": "data-contract", "label": "contract"},
                     {"key": "data-quality", "label": "dq"},
                     {"key": "code", "label": "code", "runs_tests": True}],
       "max_rounds": 3}
open(os.path.join(tmp2, "config.json"), "w").write(json.dumps(cfg))
open(os.path.join(tmp2, "backlog.md"), "w").write("# backlog\n")  # no board.json: must be created
here = os.path.dirname(os.path.abspath(__file__))


def run2(*argv, ok=True):
    r = subprocess.run([sys.executable, os.path.join(here, "board.py"), *argv],
                       env={**os.environ, "FACTORY_DIR": tmp2}, capture_output=True, text=True)
    assert (r.returncode == 0) == ok, (argv, r.stdout, r.stderr)
    return r.stdout


run2("create", "001-x", "X")
run2("stage", "001-x", "build")
run2("start-review", "001-x")
b2 = json.load(open(os.path.join(tmp2, "board.json")))
assert b2["reviewers"] == [["data-contract", "contract"], ["data-quality", "dq"], ["code", "code"]]
assert list(b2["jobs"][0]["reviews"]) == ["data-contract", "data-quality", "code"]
for k in ["data-contract", "data-quality", "code"]:
    path = os.path.join(tmp2, "jobs", "001-x", "round-1", f"review-{k}.md")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    open(path, "w").write("VERDICT: CHANGES\n- [introduced] x (a.sql:1)\n")
assert "round complete" in run2("sync", "001-x")
run2("next-round", "001-x")
run2("start-review", "001-x")
run2("next-round", "001-x")  # max_rounds 3 from config allows a third round
run2("start-review", "001-x")
run2("next-round", "001-x", ok=False)

# duplicate reviewer keys are rejected
open(os.path.join(tmp2, "config.json"), "w").write(json.dumps({"reviewers": [{"key": "a"}, {"key": "a"}]}))
run2("status", ok=False)

# ---- profile install -----------------------------------------------------------
os.makedirs(os.path.join(tmp2, "profiles", "demo"))
open(os.path.join(tmp2, "profiles", "demo", "config.json"), "w").write(json.dumps(cfg))
open(os.path.join(tmp2, "config.json"), "w").write(json.dumps(cfg) + " ")  # differs
open(os.path.join(tmp2, "board.json"), "w").write('{"jobs": []}')
run2("profile", "nope", ok=False)
run2("profile", "demo", ok=False)          # refuses to overwrite a changed config
run2("profile", "demo", "--force")
assert open(os.path.join(tmp2, "config.json")).read() == json.dumps(cfg)

# ---- D1: "Files touched" parsing: paths only (real section from job 007) ---------
SPEC_007 = """# 007: cap request body
## Files touched
- `internal/config/config.go`: `ServerConfig.MaxRequestBodyBytes`, default, Validate
- `internal/config/loader.go`: `setDefaults` entry
- `internal/config/config_test.go`: AC1 test
- `internal/server/server.go`: body-limit middleware, 413-aware helper used by 3 handlers
- `internal/server/server_bodylimit_test.go`: new, AC2 to AC4 tests
- `pkg/openai/errors.go`: `ErrRequestTooLarge`
- `config.example.yaml`: document `server.max_request_body_bytes`
- `README.md`: config block line and vision note

## Notes for reviewers
`handler.go` is mentioned here and must not count.
"""
paths, warns = board.parse_files(SPEC_007)
assert paths == sorted(["internal/config/config.go", "internal/config/loader.go",
                        "internal/config/config_test.go", "internal/server/server.go",
                        "internal/server/server_bodylimit_test.go", "pkg/openai/errors.go",
                        "config.example.yaml", "README.md"]), paths
assert warns == [], warns
# identifiers and config keys that LEAD a bullet are rejected with a warning, not recorded
paths, warns = board.parse_files("## Files touched\n- `setDefaults`: x\n- `server.port`: y\n- `a/b.go`, `Makefile`: z\n- prose bullet\n")
assert paths == ["Makefile", "a/b.go"], paths
assert len(warns) == 3, warns
for n, bad in enumerate(["no section here", "## Files touched\n- nothing useful\n"]):
    jid = f"01{n}-bad-spec"
    run("create", jid, "bad spec")  # outside the try: a setup failure must not look like a pass
    write(jid, "spec.md", bad)
    try:
        run("files", jid)
        raise AssertionError(f"accepted {bad!r}")
    except SystemExit as e:
        assert "Files touched" in str(e), e
    run("stage", jid, "merged")

# ---- D2: leftover scratch worktrees are reported ---------------------------------
root = tempfile.mkdtemp()                      # REPO is the parent of FACTORY_DIR, so keep it private
fdir = os.path.join(root, "factory")
os.makedirs(fdir)
for d in ("001-x", "001-x-scratch-code", "002-y-scratch-security"):
    os.makedirs(os.path.join(root, ".claude", "worktrees", d))
r = subprocess.run([sys.executable, os.path.join(here, "board.py"), "create", "001-x", "X"],
                   env={**os.environ, "FACTORY_DIR": fdir}, capture_output=True, text=True)
assert r.returncode == 0, r.stderr
subprocess.run([sys.executable, os.path.join(here, "board.py"), "stage", "001-x", "merged"],
               env={**os.environ, "FACTORY_DIR": fdir}, check=True)
out = subprocess.run([sys.executable, os.path.join(here, "board.py"), "worktrees"],
                     env={**os.environ, "FACTORY_DIR": fdir}, capture_output=True, text=True, check=True).stdout
assert "remove  001-x" in out, out
assert "stale-scratch  001-x-scratch-code" in out, out
assert "stale-scratch  002-y-scratch-security" in out, out
assert "stale-scratch  001-x " not in out, out  # the job's own worktree is not a scratch copy

# ---- D3: follow-ups dedupe by location (real wording from job 007) ---------------
assert board.location_key("internal/admin/handler.go:233 `handleChatAPI` decodes r.Body") == ("internal/admin/handler.go", 233)
assert board.location_key("(low) internal/admin/handler.go:377-378, 660: forms") == ("internal/admin/handler.go", 377)
assert board.location_key("internal/server/server.go:146: the 400 path echoes") == ("internal/server/server.go", 146)
assert board.location_key("The limit applies only to the group with the API routes.") is None
assert board.location_key("see server.port:8080 in the config") is None  # not a file

tmp3 = tempfile.mkdtemp()
open(os.path.join(tmp3, "backlog.md"), "w").write("# backlog\n")
env3 = {**os.environ, "FACTORY_DIR": tmp3}
def run3(*argv):
    r = subprocess.run([sys.executable, os.path.join(here, "board.py"), *argv], env=env3, capture_output=True, text=True)
    assert r.returncode == 0, (argv, r.stdout, r.stderr)
    return r
run3("create", "020-fu", "follow-ups"); run3("stage", "020-fu", "build"); run3("start-review", "020-fu")
rd = os.path.join(tmp3, "jobs", "020-fu", "round-1"); os.makedirs(rd, exist_ok=True)
open(os.path.join(rd, "review-security.md"), "w").write(
    "VERDICT: PASS\n- [pre-existing] (low) internal/admin/handler.go:233 `handleChatAPI` decodes `r.Body` without a cap.\n"
    "- [pre-existing] (low) internal/admin/handler.go:377-378, 660: the login form uses the default 10 MB cap.\n"
    "- [pre-existing] (low) internal/server/server.go:146: the 400 path echoes err.Error().\n")
open(os.path.join(rd, "review-ux.md"), "w").write(
    "VERDICT: PASS\n- [pre-existing] The limit applies only to the group with the API routes.\n")
open(os.path.join(rd, "review-code.md"), "w").write(
    "VERDICT: PASS\n- [pre-existing] internal/admin/handler.go:233 handleChatAPI decodes an uncapped body (out of scope).\n")
r = run3("followups", "020-fu")
assert "3 follow-up(s) added" in r.stdout and "1 duplicate(s) skipped" in r.stdout, r.stdout
assert "no path:line" in r.stderr and "ux review" in r.stderr, r.stderr
lines = [l for l in open(os.path.join(tmp3, "backlog.md")).read().splitlines() if l.startswith("- [ ]")]
assert len(lines) == 3, lines
assert sum("handler.go:233" in l for l in lines) == 1
assert "The limit applies only" not in "".join(lines)
r = run3("followups", "020-fu")                      # idempotent: a second run adds nothing
assert "0 follow-up(s) added" in r.stdout, r.stdout
assert len([l for l in open(os.path.join(tmp3, "backlog.md")).read().splitlines() if l.startswith("- [ ]")]) == 3

# ---- D4: backlog.md is created when missing --------------------------------------
for with_example in (False, True):
    d4 = tempfile.mkdtemp()
    if with_example:
        open(os.path.join(d4, "backlog.example.md"), "w").write("# From example\n\n- [ ] sample item\n")
    env4 = {**os.environ, "FACTORY_DIR": d4}
    r = subprocess.run([sys.executable, os.path.join(here, "board.py"), "create", "030-bl", "backlog"],
                       env=env4, capture_output=True, text=True)
    assert r.returncode == 0, r.stderr
    text = open(os.path.join(d4, "backlog.md")).read()          # created without us writing it
    assert ("From example" in text) if with_example else text.startswith("# Factory backlog"), text
    open(os.path.join(d4, "backlog.md"), "a").write("- [x] ticked by hand\n")  # an existing file is never overwritten
    subprocess.run([sys.executable, os.path.join(here, "board.py"), "create", "031-bl", "again"], env=env4, check=True)
    assert "ticked by hand" in open(os.path.join(d4, "backlog.md")).read()

print("ok: board.py self-check passed")
