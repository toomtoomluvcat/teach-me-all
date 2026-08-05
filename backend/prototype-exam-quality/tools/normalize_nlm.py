#!/usr/bin/env python3
"""Normalize NotebookLM markdown exports into the strict labelkit shape."""

from __future__ import annotations

import argparse
import re
from pathlib import Path

QUESTION_RE = re.compile(
    r"^\*{0,2}\s*(?:question|ข้อ(?:ที่)?)\s+\d+\s*\*{0,2}\s*$",
    re.IGNORECASE,
)
OPTION_RE = re.compile(
    r"^\s*(?:[*+-]\s+)?\*{0,2}\s*([A-D])\s*[\.\)\:\-–]\s*(.*?)\s*\*{0,2}\s*$",
    re.IGNORECASE,
)
ANSWER_RE = re.compile(
    r"^\*{0,2}\s*(?:correct answer|answer|คำตอบ(?:ที่ถูก(?:ต้อง)?)?|เฉลย)"
    r"\s*(?:คือ\s*)?[:\-–：]\s*\(?\s*([A-D])\b",
    re.IGNORECASE,
)


def clean_line(raw: str) -> str:
    text = raw.replace("\u00a0", " ").strip()
    text = re.sub(r"^\s*(?:[*+-]\s+)", "", text)
    text = text.replace("**", "").replace("__", "").replace(chr(96), "")
    return re.sub(r"\s+", " ", text).strip()


def normalize(path: Path) -> str:
    raw_lines = path.read_text(encoding="utf-8-sig").splitlines()
    lines = [clean_line(line) for line in raw_lines]
    starts = [i for i, line in enumerate(lines) if QUESTION_RE.match(line)]
    if not starts:
        raise ValueError(f"no Question/ข้อ headers found in {path}")

    output: list[str] = []
    for question_no, start in enumerate(starts, start=1):
        end = starts[question_no] if question_no < len(starts) else len(lines)
        block = lines[start + 1 : end]
        option_positions = [i for i, line in enumerate(block) if OPTION_RE.match(line)]
        if len(option_positions) < 4:
            raise ValueError(f"question {question_no} has fewer than four options")
        first = option_positions[0]
        option_lines = block[first : first + 4]
        if any(OPTION_RE.match(line) is None for line in option_lines):
            raise ValueError(f"question {question_no} options are not contiguous")

        stem_parts = [line for line in block[:first] if line and not line.startswith("#")]
        if not stem_parts:
            raise ValueError(f"question {question_no} has no stem")

        answer = None
        for line in block[first + 4 :]:
            match = ANSWER_RE.match(line)
            if match:
                answer = match.group(1).upper()
                break

        output.append(f"{question_no}. {' '.join(stem_parts)}")
        output.extend(
            f"{match.group(1).upper()}. {match.group(2).strip()}"
            for line in option_lines
            if (match := OPTION_RE.match(line))
        )
        if answer:
            output.append(f"Answer: {answer}")
        output.append("")

    return "\n".join(output).rstrip() + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(normalize(args.source), encoding="utf-8")
    print(f"wrote {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
