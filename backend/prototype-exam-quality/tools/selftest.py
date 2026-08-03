#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""End-to-end smoke test for labelkit, run against tools/testdata/.

    py tools\\selftest.py

Exercises: the happy path (parse -> build -> score), the two malformed-input
paths, determinism of the shuffle, and the promise that the sheet leaks
neither the answer nor the provenance.
"""

from __future__ import annotations

import csv
import json
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
DATA = HERE / "testdata"
KIT = HERE / "labelkit.py"

FAILURES = []


def run(*args):
    proc = subprocess.run(
        [sys.executable, str(KIT), *args],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    return proc.returncode, (proc.stdout or "") + (proc.stderr or "")


def check(name, ok, detail=""):
    print("%-58s %s" % (name, "ok" if ok else "FAIL"))
    if not ok:
        FAILURES.append("%s%s" % (name, ("\n    " + detail.strip()) if detail else ""))


def main():
    tmp = Path(tempfile.mkdtemp(prefix="labelkit-selftest-"))

    # --- parser, happy path --------------------------------------------------
    code, out = run("parse", str(DATA / "nlm-pages.txt"), "--dry-run")
    check("parse nlm-pages.txt --dry-run exits 0", code == 0, out)
    check("dry-run reports 4 questions", "Parsed 4 question" in out, out)

    code, out = run("parse", str(DATA / "nlm-book.txt"), "--dry-run")
    check("parse Thai nlm-book.txt exits 0", code == 0, out)
    check("Thai answer line understood", "*B." in out or "*C." in out, out)

    # --- parser, malformed paths --------------------------------------------
    code, out = run("parse", str(DATA / "malformed.txt"))
    check("three-option question exits non-zero", code != 0, out)
    check("error names a line number", "at line" in out, out)
    check("error prints the raw block", "raw block:" in out, out)

    code, out = run("parse", str(DATA / "malformed-preamble.txt"))
    check("leading commentary exits non-zero", code != 0, out)

    # --- build ---------------------------------------------------------------
    build_args = [
        "build",
        "--ours", str(DATA / "ours.json"),
        "--nlm-pages", str(DATA / "nlm-pages.txt"),
        "--nlm-book", str(DATA / "nlm-book.txt"),
        "--seed", "1234",
        "--out", str(tmp / "a"),
    ]
    code, out = run(*build_args)
    check("build exits 0", code == 0, out)
    check("build excludes the gate-failed question", "1 of our questions" in out, out)

    sheet = tmp / "a" / "sheet.csv"
    key = json.loads((tmp / "a" / "key.json").read_text(encoding="utf-8"))

    raw = sheet.read_bytes()
    check("sheet.csv is utf-8 with BOM", raw.startswith(b"\xef\xbb\xbf"), repr(raw[:8]))

    with sheet.open(encoding="utf-8-sig", newline="") as fh:
        rows = list(csv.DictReader(fh))
    check("sheet has 12 rows", len(rows) == 12, str(len(rows)))
    check(
        "sheet columns are exactly the agreed seven",
        list(rows[0].keys())
        == ["id", "stem", "choice_a", "choice_b", "choice_c", "choice_d", "label"],
        str(list(rows[0].keys())),
    )
    check("ids are Q01..Q12", [r["id"] for r in rows] == ["Q%02d" % i for i in range(1, 13)])
    check("label column is empty", all(r["label"] == "" for r in rows))

    # The sheet must not leak the answer, the explanation or the quote.
    ours = json.loads((DATA / "ours.json").read_text(encoding="utf-8"))
    leak_strings = [q["explanation"] for q in ours["questions"]]
    leak_strings += [q["source_quote"] for q in ours["questions"]]
    body = sheet.read_text(encoding="utf-8-sig")
    check(
        "sheet contains no explanation or source_quote",
        not any(s and s in body for s in leak_strings),
    )
    check(
        "sheet contains no provenance words",
        not any(w in body for w in ("ours", "nlm-pages", "nlm-book", "is_correct")),
    )

    # Sources must be interleaved, or the shuffle did nothing.
    order = [key["questions"][r["id"]]["source"] for r in rows]
    check("shuffle interleaves the three sources", len(set(order[:4])) > 1, str(order))

    # --- determinism ---------------------------------------------------------
    code, _ = run(*(build_args[:-1] + [str(tmp / "b")]))
    same_seed = (tmp / "b" / "sheet.csv").read_bytes() == raw
    check("same seed reproduces the sheet byte for byte", code == 0 and same_seed)

    alt = list(build_args)
    alt[alt.index("--seed") + 1] = "9999"
    alt[-1] = str(tmp / "c")
    run(*alt)
    check(
        "a different seed produces a different order",
        (tmp / "c" / "sheet.csv").read_bytes() != raw,
    )

    # --- score ---------------------------------------------------------------
    # Fill labels so that `ours` wins by exactly the pass margin.
    plan = {
        "ours": ["good"] * 4,
        "nlm-pages": ["off_topic", "ambiguous", "true_distractor", "no_reading_needed"],
        "nlm-book": ["teacher_guide", "off_topic", "ambiguous", "no_reading_needed"],
    }
    used = {s: 0 for s in plan}
    filled = tmp / "filled.csv"
    with filled.open("w", encoding="utf-8-sig", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        for r in rows:
            src = key["questions"][r["id"]]["source"]
            r = dict(r)
            r["label"] = plan[src][used[src]]
            used[src] += 1
            writer.writerow(r)

    code, out = run("score", "--sheet", str(filled), "--key", str(tmp / "a" / "key.json"))
    check("score on a fully-labelled sheet exits 0", code == 0, out)
    check("verdict is PASS when ours wins by 4", "VERDICT: PASS" in out, out)
    check("score prints the n= caveat", "NOT KNOWN" in out, out)

    # A bad label must be rejected by row.
    bad = tmp / "bad.csv"
    text = filled.read_text(encoding="utf-8-sig").replace("good", "GREAT", 1)
    bad.write_text(text, encoding="utf-8-sig", newline="")
    code, out = run("score", "--sheet", str(bad), "--key", str(tmp / "a" / "key.json"))
    check("an unknown label exits non-zero", code != 0, out)
    check("the offending row is named", "row " in out and "GREAT" in out, out)

    # An empty label must be rejected too.
    blank = tmp / "blank.csv"
    lines = filled.read_text(encoding="utf-8-sig").splitlines()
    lines[1] = lines[1].rsplit(",", 1)[0] + ","
    blank.write_text("\n".join(lines) + "\n", encoding="utf-8-sig", newline="")
    code, out = run("score", "--sheet", str(blank), "--key", str(tmp / "a" / "key.json"))
    check("an empty label exits non-zero", code != 0, out)

    # Case-insensitivity is promised.
    upper = tmp / "upper.csv"
    upper.write_text(
        filled.read_text(encoding="utf-8-sig").replace("good", "Good"),
        encoding="utf-8-sig",
        newline="",
    )
    code, out = run("score", "--sheet", str(upper), "--key", str(tmp / "a" / "key.json"))
    check("labels are case-insensitive", code == 0 and "VERDICT: PASS" in out, out)

    print("")
    if FAILURES:
        print("%d FAILED" % len(FAILURES))
        for f in FAILURES:
            print("  - %s" % f)
        return 1
    print("all checks passed")
    return 0


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    sys.exit(main())
