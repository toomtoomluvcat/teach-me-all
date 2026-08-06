#!/usr/bin/env python3
"""Check whether longer questions pass gates more often (length bias).

Hypothesis: questions with longer stems/choices tend to PASS the deterministic
gates. If true, the gate may be rewarding verbosity (more metadata, more
quote overlap) rather than question quality.
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, ".."))

REPORTS = [
    ("physics", ".scratch/e0312e3e107a1953/benchmark-all.json"),
    ("physics-fix", ".scratch/e0312e3e107a1953/benchmark-applicationhard.json"),
    ("chemistry", ".scratch/a4508f0287624222/benchmark-calculation.json"),
    ("economics", ".scratch/411dd75621fa43f9/benchmark-all.json"),
    ("us-history", ".scratch/bc9cce4c7060c17e/benchmark-all.json"),
    ("biology", ".scratch/87040807ef7f73b8/benchmark-all.json"),
]


def load_rows():
    rows = []
    for label, rel in REPORTS:
        path = os.path.join(ROOT, rel)
        if not os.path.exists(path):
            continue
        with open(path, encoding="utf-8") as fh:
            rep = json.load(fh)
        for case in rep.get("cases", []):
            for q in (case.get("questions") or []):
                stem = len(q.get("stem", ""))
                choices = sum(len(c.get("content", "")) for c in q.get("choices", []))
                expl = len(q.get("explanation", ""))
                changed = len(q.get("changed_condition") or "")
                steps = len(q.get("reasoning_steps") or [])
                dr = len(q.get("distractor_reasons") or [])
                quote = len(q.get("source_quote", ""))
                total = stem + choices + expl
                rows.append({
                    "passed": bool(q.get("passed")),
                    "case": case.get("name"),
                    "subject": label,
                    "stem": stem,
                    "choices": choices,
                    "expl": expl,
                    "changed": changed,
                    "steps": steps,
                    "dr": dr,
                    "quote": quote,
                    "total": total,
                })
    return rows


def avg(xs):
    return round(sum(xs) / len(xs), 1) if xs else 0.0


def pct(xs):
    return round(100.0 * sum(xs) / len(xs), 1) if xs else 0.0


def main():
    rows = load_rows()
    passed = [r for r in rows if r["passed"]]
    failed = [r for r in rows if not r["passed"]]
    print(f"total={len(rows)}  passed={len(passed)} ({pct([1]*len(passed))}%)  failed={len(failed)}")
    print()
    print(f"{'metric':<12}{'passed':>10}{'failed':>10}{'P/F ratio':>10}")
    for key, name in [
        ("stem", "stem_len"), ("choices", "choices_len"),
        ("expl", "expl_len"), ("total", "total_len"),
        ("changed", "changed_len"), ("steps", "reasoning_n"),
        ("dr", "distractor_n"), ("quote", "quote_len"),
    ]:
        p = avg([r[key] for r in passed])
        fl = avg([r[key] for r in failed])
        ratio = round(p / fl, 2) if fl else 0.0
        print(f"{name:<12}{p:>10}{fl:>10}{ratio:>10}")

    # by category: do longer stems correlate with pass within the same case?
    print()
    print("per-case stem length passed vs failed:")
    cases = {}
    for r in rows:
        cases.setdefault(r["case"], []).append(r)
    for case, rs in sorted(cases.items()):
        p = [r for r in rs if r["passed"]]
        f = [r for r in rs if not r["passed"]]
        pp = avg([r["stem"] for r in p])
        ff = avg([r["stem"] for r in f])
        mark = "  <<<" if pp > ff * 1.2 else ""
        print(f"  {case:<20} passed_stem={pp:>7} failed_stem={ff:>7}{mark}")

    # distribution of passed/failed by stem length quartile (overall)
    print()
    all_stems = sorted(r["stem"] for r in rows)
    n = len(all_stems)
    qs = [all_stems[n // 4], all_stems[n // 2], all_stems[3 * n // 4]]
    print(f"stem length quartiles: {qs}")
    for label, lo, hi in [("Q1(short)", 0, qs[0]), ("Q2", qs[0], qs[1]), ("Q3", qs[1], qs[2]), ("Q4(long)", qs[2], 10**9)]:
        band = [r for r in rows if lo <= r["stem"] < hi]
        if not band:
            continue
        p = [r for r in band if r["passed"]]
        print(f"  {label:<10} n={len(band):>3} pass={pct([1]*len(p))}%")


if __name__ == "__main__":
    main()
