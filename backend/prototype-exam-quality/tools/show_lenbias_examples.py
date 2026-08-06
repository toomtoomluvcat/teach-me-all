#!/usr/bin/env python3
"""Print per-question correct/avg-wrong length ratios from a benchmark report,
flagging questions whose keyed choice is disproportionately long."""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, ".."))


def main():
    rel = sys.argv[1] if len(sys.argv) > 1 else ".scratch/411dd75621fa43f9/benchmark-all.json"
    case_filter = sys.argv[2] if len(sys.argv) > 2 else None
    with open(os.path.join(ROOT, rel), encoding="utf-8") as fh:
        rep = json.load(fh)
    for c in rep.get("cases", []):
        if case_filter and c.get("name") != case_filter:
            continue
        print(f"== {c.get('name')} ==")
        for q in (c.get("questions") or []):
            if not q.get("passed"):
                continue
            choices = q.get("choices", [])
            cl = [len(ch.get("content", "")) for ch in choices]
            ci = next((i for i, ch in enumerate(choices) if ch.get("is_correct")), None)
            if ci is None:
                continue
            wrong = [cl[i] for i in range(len(cl)) if i != ci]
            wa = sum(wrong) / len(wrong) if wrong else 0
            ratio = round(cl[ci] / wa, 2) if wa else 0
            flag = "  <== LENBIAS" if ratio > 1.4 else ""
            print(f"  ratio={ratio} correct={cl[ci]} avgWrong={int(wa)}{flag}")
            print(f"    stem:    {q.get('stem','')[:70]}")
            print(f"    correct: {choices[ci]['content'][:70]}")
        print()


if __name__ == "__main__":
    main()
