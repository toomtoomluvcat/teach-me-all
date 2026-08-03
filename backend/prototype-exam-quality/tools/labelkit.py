#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Blind-labelling toolkit for the question-quality comparison.

Three commands:

    build   merge our JSON run files with two NotebookLM .txt dumps,
            shuffle deterministically, emit sheet.csv + key.json
    parse   validate a NotebookLM .txt on its own (--dry-run to eyeball it)
    score   read the filled sheet plus the key and report the comparison

Standard library only. No network. Windows-friendly: every CSV it writes is
utf-8-sig so Excel opens Thai text correctly.

The parser deliberately refuses to guess. NotebookLM's export shape has not
been seen yet, so anything it cannot read as exactly one stem and exactly four
choices is an error that names the line and prints the raw block. Silently
mis-reading 15 questions would destroy the measurement this toolkit exists to
take, so there is no padding, no truncating and no skipping.
"""

from __future__ import annotations

import argparse
import csv
import json
import random
import re
import sys
import unicodedata
from pathlib import Path

SOURCES = ("ours", "nlm-pages", "nlm-book")

LABELS = (
    "good",
    "true_distractor",
    "off_topic",
    "no_reading_needed",
    "teacher_guide",
    "ambiguous",
)

CHOICE_LETTERS = ("A", "B", "C", "D")

# Difference in `good` counts that the pre-agreed verdict treats as a real gap.
PASS_MARGIN = 4


class ParseError(Exception):
    """A NotebookLM file we refuse to read. Carries enough to fix the file."""

    def __init__(self, message, path, lineno=None, block=None):
        super().__init__(message)
        self.message = message
        self.path = path
        self.lineno = lineno
        self.block = block or []

    def render(self):
        out = ["", "PARSE ERROR in %s" % self.path]
        if self.lineno is not None:
            out.append("  at line %d" % self.lineno)
        out.append("  %s" % self.message)
        if self.block:
            out.append("")
            out.append("  raw block:")
            for lineno, raw in self.block:
                out.append("  %5d | %s" % (lineno, raw))
        out.append("")
        out.append("  Fix the .txt by hand and run `parse --dry-run` again.")
        out.append("  Nothing was written.")
        return "\n".join(out)


# --- NotebookLM text parsing -------------------------------------------------

# Option-label alphabets, in the order we try them. Latin first so that a file
# using "A. B. C. D." is never read as digits.
LABEL_STYLES = (
    ("latin-upper", ("A", "B", "C", "D")),
    ("latin-lower", ("a", "b", "c", "d")),
    ("thai", ("ก", "ข", "ค", "ง")),  # ก ข ค ง
    ("digit", ("1", "2", "3", "4")),
)

# Every label token we will accept on an "Answer:" line, mapped to its index.
ANSWER_TOKENS = {}
for _style, _labels in LABEL_STYLES:
    for _i, _lab in enumerate(_labels):
        ANSWER_TOKENS.setdefault(_lab, _i)
        ANSWER_TOKENS.setdefault(_lab.lower(), _i)
        ANSWER_TOKENS.setdefault(_lab.upper(), _i)

QUESTION_RE = re.compile(
    r"^(?:(?:ข้อ(?:ที่)?"  # ข้อ / ข้อที่
    r"|คำถาม(?:ที่)?"  # คำถาม / คำถามที่
    r"|Question|Q)\s*)?\d+\s*[\.\)\:\-–]\s*\S",
    re.IGNORECASE,
)

ANSWER_RE = re.compile(
    r"^(?:answer|ans|correct answer|correct|key"
    r"|เฉลย"  # เฉลย
    r"|คำตอบ(?:ที่ถูก(?:ต้อง)?)?"  # คำตอบ / คำตอบที่ถูก(ต้อง)
    r")\s*(?:คือ\s*)?[:\-–：]\s*(.+)$",  # คือ / : / - / ：
    re.IGNORECASE,
)

BULLET_RE = re.compile(r"^[\-\*\+•‣●○·]\s+")
BLOCKQUOTE_RE = re.compile(r"^>+\s*")


def clean_line(raw):
    """Strip markdown decoration without touching the words."""
    text = raw.replace(" ", " ")
    text = "".join(ch for ch in text if ch not in "​‌‍﻿")
    text = text.strip()
    # Blockquote and bullet markers may be nested: "> - **A.** text".
    for _ in range(4):
        before = text
        text = BLOCKQUOTE_RE.sub("", text)
        text = BULLET_RE.sub("", text)
        if text == before:
            break
    text = text.replace("**", "").replace("__", "").replace("`", "")
    text = re.sub(r"\s+", " ", text).strip()
    # A bold marker may have wrapped the whole line: "*A. text*".
    if len(text) > 2 and text.startswith("*") and text.endswith("*"):
        text = text[1:-1].strip()
    return text


def option_match(text, labels):
    """Return (label_index, option_text) if `text` is an option line."""
    for i, label in enumerate(labels):
        # Digits need a space after the separator, or "1.5 grams" reads as an
        # option. Letters do not, because "A.text" happens in real pastes.
        gap = r"\s+" if label.isdigit() else r"\s*"
        pattern = r"^\(?\s*%s\s*\)?\s*[\.\)\:\-–]?%s(\S.*)$" % (re.escape(label), gap)
        m = re.match(pattern, text)
        if m:
            # Require *some* separator or bracket, otherwise a stem beginning
            # with the bare letter "A" would match.
            head = text[: len(text) - len(m.group(1))]
            if not re.search(r"[\.\)\:\-–\(]", head):
                continue
            return i, m.group(1).strip()
    return None


def answer_match(text):
    m = ANSWER_RE.match(text)
    if not m:
        return None
    return m.group(1).strip()


def find_quads(lines, labels):
    """Locate runs of four consecutive option lines labelled 1st..4th."""
    quads = []
    i = 0
    while i < len(lines):
        first = option_match(lines[i]["clean"], labels)
        if first is not None and first[0] == 0 and i + 3 < len(lines):
            run = [first]
            ok = True
            for step in range(1, 4):
                nxt = option_match(lines[i + step]["clean"], labels)
                if nxt is None or nxt[0] != step:
                    ok = False
                    break
                run.append(nxt)
            if ok:
                quads.append((i, [text for _, text in run]))
                i += 4
                continue
        i += 1
    return quads


def raw_block(lines, start, end):
    """Raw source lines for the inclusive index range, for error messages."""
    return [(lines[i]["lineno"], lines[i]["raw"]) for i in range(start, min(end + 1, len(lines)))]


def parse_notebooklm(path):
    """Parse a NotebookLM .txt into a list of question dicts. Raises ParseError.

    Returns [{"stem": str, "choices": [str x4], "correct": int|None,
              "lineno": int}].
    """
    text = Path(path).read_text(encoding="utf-8-sig")
    lines = []
    for n, raw in enumerate(text.splitlines(), start=1):
        cleaned = clean_line(raw)
        if cleaned:
            lines.append({"lineno": n, "raw": raw.rstrip("\r\n"), "clean": cleaned})

    if not lines:
        raise ParseError("file is empty after stripping markdown", path)

    # Pick the labelling style that yields the most complete four-option runs.
    best_style, best_labels, best_quads = None, None, []
    for style, labels in LABEL_STYLES:
        quads = find_quads(lines, labels)
        if len(quads) > len(best_quads):
            best_style, best_labels, best_quads = style, labels, quads

    if not best_quads:
        raise ParseError(
            "found no complete group of four option lines in any recognised "
            "labelling style (A-D, a-d, %s, 1-4). The file may use a shape this "
            "parser has not been taught." % "-".join(LABEL_STYLES[2][1][:1] + LABEL_STYLES[2][1][-1:]),
            path,
            lineno=lines[0]["lineno"],
            block=raw_block(lines, 0, min(9, len(lines) - 1)),
        )

    questions = []
    cursor = 0  # first unconsumed index in `lines`
    for qi, (quad_start, choices) in enumerate(best_quads):
        quad_end = quad_start + 3
        pre = lines[cursor:quad_start]
        block_start = cursor if pre else quad_start

        # --- the stem -------------------------------------------------------
        candidates = []
        for idx in range(cursor, quad_start):
            line = lines[idx]
            if answer_match(line["clean"]) is not None:
                raise ParseError(
                    "an answer line appears before the options of question %d; "
                    "this parser cannot tell which question it belongs to"
                    % (qi + 1),
                    path,
                    lineno=line["lineno"],
                    block=raw_block(lines, block_start, quad_end),
                )
            candidates.append(idx)

        if not candidates:
            raise ParseError(
                "found four options with no question text before them "
                "(question %d)" % (qi + 1),
                path,
                lineno=lines[quad_start]["lineno"],
                block=raw_block(lines, quad_start, quad_end),
            )

        if len(candidates) == 1:
            stem_idx = candidates
        else:
            marked = [i for i in candidates if QUESTION_RE.match(lines[i]["clean"])]
            if len(marked) != 1:
                looks_like_option = [
                    i for i in candidates if option_match(lines[i]["clean"], best_labels) is not None
                ]
                extra = ""
                if looks_like_option:
                    extra = (
                        " Line %d looks like an option but is not part of a "
                        "complete four-option group -- the question may have "
                        "more or fewer than four choices."
                        % lines[looks_like_option[0]]["lineno"]
                    )
                raise ParseError(
                    "question %d has %d lines of text before its options, and "
                    "%d of them look like a numbered question start, so the stem "
                    "is ambiguous. Delete any heading or commentary, or merge the "
                    "stem onto one numbered line.%s"
                    % (qi + 1, len(candidates), len(marked), extra),
                    path,
                    lineno=lines[candidates[0]]["lineno"],
                    block=raw_block(lines, block_start, quad_end),
                )
            start = candidates.index(marked[0])
            if start != 0:
                raise ParseError(
                    "question %d has %d line(s) before its numbered question "
                    "line. Leading headings or leftover text must be deleted; "
                    "this parser will not decide for you what is a stem."
                    % (qi + 1, start),
                    path,
                    lineno=lines[candidates[0]]["lineno"],
                    block=raw_block(lines, block_start, quad_end),
                )
            # A stem wrapped over several lines is joined; nothing is dropped.
            stem_idx = candidates[start:]

        stem = " ".join(lines[i]["clean"] for i in stem_idx)
        stem = re.sub(r"^(?:(?:ข้อ(?:ที่)?|"
                      r"คำถาม(?:ที่)?|"
                      r"Question|Q)\s*)?\d+\s*[\.\)\:\-–]\s*", "", stem,
                      flags=re.IGNORECASE).strip()
        if not stem:
            raise ParseError(
                "question %d has a number but no question text" % (qi + 1),
                path,
                lineno=lines[stem_idx[0]]["lineno"],
                block=raw_block(lines, block_start, quad_end),
            )

        # --- the optional answer line ---------------------------------------
        next_start = best_quads[qi + 1][0] if qi + 1 < len(best_quads) else len(lines)
        correct = None
        after = quad_end + 1
        while after < next_start:
            token = answer_match(lines[after]["clean"])
            if token is None:
                break
            if correct is not None:
                raise ParseError(
                    "question %d has more than one answer line" % (qi + 1),
                    path,
                    lineno=lines[after]["lineno"],
                    block=raw_block(lines, block_start, after),
                )
            head = re.split(r"[\s\.\)\:–,]", token.strip().lstrip("(").lstrip(), maxsplit=1)[0]
            head = head.strip("()[]. ")
            if head not in ANSWER_TOKENS:
                raise ParseError(
                    "question %d has an answer line whose choice %r is not one "
                    "of A-D, a-d, %s or 1-4"
                    % (qi + 1, token.strip(), "/".join(LABEL_STYLES[2][1])),
                    path,
                    lineno=lines[after]["lineno"],
                    block=raw_block(lines, block_start, after),
                )
            correct = ANSWER_TOKENS[head]
            after += 1

        if any(not c for c in choices):
            raise ParseError(
                "question %d has an empty choice" % (qi + 1),
                path,
                lineno=lines[quad_start]["lineno"],
                block=raw_block(lines, quad_start, quad_end),
            )

        questions.append(
            {
                "stem": stem,
                "choices": choices,
                "correct": correct,
                "lineno": lines[stem_idx[0]]["lineno"],
            }
        )
        cursor = after

    if cursor < len(lines):
        raise ParseError(
            "%d line(s) after the last question could not be read as part of "
            "any question. Delete trailing commentary, or fix the malformed "
            "question they belong to." % (len(lines) - cursor),
            path,
            lineno=lines[cursor]["lineno"],
            block=raw_block(lines, cursor, len(lines) - 1),
        )

    return questions


# --- our JSON ----------------------------------------------------------------


def load_ours(paths, include_failed):
    """Read one or more emitted run files into the same question shape."""
    out = []
    dropped = 0
    for path in paths:
        data = json.loads(Path(path).read_text(encoding="utf-8-sig"))
        if isinstance(data, dict):
            raw_questions = data.get("questions")
        elif isinstance(data, list):
            raw_questions = data
        else:
            raise SystemExit("ERROR: %s is neither a run object nor a list" % path)
        if not raw_questions:
            raise SystemExit("ERROR: %s has no questions" % path)

        for i, q in enumerate(raw_questions, start=1):
            if not include_failed and q.get("passed") is False:
                dropped += 1
                continue
            choices = q.get("choices") or []
            if len(choices) != 4:
                raise SystemExit(
                    "ERROR: %s question %d has %d choices, expected 4"
                    % (path, i, len(choices))
                )
            correct = [j for j, c in enumerate(choices) if c.get("is_correct")]
            if len(correct) != 1:
                raise SystemExit(
                    "ERROR: %s question %d has %d correct choices, expected 1"
                    % (path, i, len(correct))
                )
            stem = (q.get("stem") or "").strip()
            if not stem:
                raise SystemExit("ERROR: %s question %d has an empty stem" % (path, i))
            out.append(
                {
                    "stem": stem,
                    "choices": [(c.get("content") or "").strip() for c in choices],
                    "correct": correct[0],
                    "lineno": None,
                }
            )
    return out, dropped


# --- build -------------------------------------------------------------------


def flatten(text):
    """One cell, one line. Newlines in a CSV cell make the sheet unreadable."""
    return re.sub(r"\s+", " ", (text or "").replace(" ", " ")).strip()


def cmd_build(args):
    ours, dropped = load_ours(args.ours, args.include_failed)
    pages = parse_notebooklm(args.nlm_pages)
    book = parse_notebooklm(args.nlm_book)

    by_source = {"ours": ours, "nlm-pages": pages, "nlm-book": book}

    records = []
    for source in SOURCES:  # fixed order in, so the shuffle is the only variable
        for i, q in enumerate(by_source[source], start=1):
            if len(q["choices"]) != 4:
                raise SystemExit(
                    "ERROR: %s question %d has %d choices, expected 4"
                    % (source, i, len(q["choices"]))
                )
            records.append({"source": source, "original_index": i, "q": q})

    rng = random.Random(args.seed)  # seeded: no wall-clock randomness anywhere
    rng.shuffle(records)

    width = max(2, len(str(len(records))))
    outdir = Path(args.out)
    outdir.mkdir(parents=True, exist_ok=True)
    sheet_path = outdir / "sheet.csv"
    key_path = outdir / "key.json"

    key = {
        "seed": args.seed,
        "counts": {s: len(by_source[s]) for s in SOURCES},
        "inputs": {
            "ours": [str(p) for p in args.ours],
            "nlm-pages": str(args.nlm_pages),
            "nlm-book": str(args.nlm_book),
        },
        "questions": {},
    }

    with sheet_path.open("w", encoding="utf-8-sig", newline="") as fh:
        writer = csv.writer(fh)
        writer.writerow(
            ["id", "stem", "choice_a", "choice_b", "choice_c", "choice_d", "label"]
        )
        for n, rec in enumerate(records, start=1):
            qid = "Q%0*d" % (width, n)
            q = rec["q"]
            # Sheet carries the stem and the four choices, in the order they
            # were produced, and nothing else. No answer, no explanation, no
            # source quote, no provenance.
            writer.writerow([qid, flatten(q["stem"])] + [flatten(c) for c in q["choices"]] + [""])
            key["questions"][qid] = {
                "source": rec["source"],
                "original_index": rec["original_index"],
                "correct_choice": (
                    CHOICE_LETTERS[q["correct"]] if q["correct"] is not None else None
                ),
            }

    key_path.write_text(
        json.dumps(key, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )

    print("wrote %s  (%d questions)" % (sheet_path, len(records)))
    print("wrote %s" % key_path)
    print("")
    for s in SOURCES:
        n_unknown = sum(1 for q in by_source[s] if q["correct"] is None)
        note = "  (%d without a stated answer)" % n_unknown if n_unknown else ""
        print("  %-10s %3d questions%s" % (s, len(by_source[s]), note))
    if dropped:
        print("")
        print(
            "  note: %d of our questions were excluded because they failed a gate."
            % dropped
        )
        print("        Pass --include-failed to label those too.")
    counts = {len(by_source[s]) for s in SOURCES}
    if len(counts) != 1:
        print("")
        print("  WARNING: the three sets are not the same size. The comparison")
        print("           assumes equal n per set; fix the inputs before labelling.")
    print("")
    print("Now open %s, fill the `label` column, and run `score`." % sheet_path)
    print("Do not open key.json until the sheet is filled.")
    return 0


# --- parse -------------------------------------------------------------------


def cmd_parse(args):
    questions = parse_notebooklm(args.path)
    if args.dry_run:
        print("Parsed %d question(s) from %s" % (len(questions), args.path))
        print("")
        for i, q in enumerate(questions, start=1):
            print("%2d. (line %d) %s" % (i, q["lineno"], q["stem"]))
            for j, choice in enumerate(q["choices"]):
                mark = " *" if q["correct"] == j else "  "
                print("     %s%s. %s" % (mark, CHOICE_LETTERS[j], choice))
            if q["correct"] is None:
                print("      (no answer line found)")
            print("")
        print("* marks the stated correct answer.")
        print("Eyeball the above before running `build`.")
    else:
        stated = sum(1 for q in questions if q["correct"] is not None)
        print(
            "OK: %s parsed cleanly -- %d questions, %d with a stated answer."
            % (args.path, len(questions), stated)
        )
    return 0


# --- score -------------------------------------------------------------------


def cmd_score(args):
    key = json.loads(Path(args.key).read_text(encoding="utf-8-sig"))
    key_questions = key.get("questions") or {}
    if not key_questions:
        raise SystemExit("ERROR: %s has no questions" % args.key)

    rows = []
    with Path(args.sheet).open("r", encoding="utf-8-sig", newline="") as fh:
        reader = csv.DictReader(fh)
        if reader.fieldnames is None or "id" not in reader.fieldnames or "label" not in reader.fieldnames:
            raise SystemExit(
                "ERROR: %s must have `id` and `label` columns (found: %s)"
                % (args.sheet, reader.fieldnames)
            )
        for lineno, row in enumerate(reader, start=2):
            rows.append((lineno, (row.get("id") or "").strip(), (row.get("label") or "").strip()))

    problems = []
    seen = set()
    for lineno, qid, label in rows:
        if not qid:
            problems.append("row %d: missing id" % lineno)
            continue
        if qid in seen:
            problems.append("row %d: duplicate id %s" % (lineno, qid))
        seen.add(qid)
        if qid not in key_questions:
            problems.append("row %d: id %s is not in the key" % (lineno, qid))
        if not label:
            problems.append("row %d (%s): label is empty" % (lineno, qid))
        elif label.lower() not in LABELS:
            problems.append(
                "row %d (%s): %r is not a valid label" % (lineno, qid, label)
            )
    for qid in key_questions:
        if qid not in seen:
            problems.append("%s is in the key but missing from the sheet" % qid)

    if problems:
        print("The sheet is not ready to score:", file=sys.stderr)
        for p in problems:
            print("  %s" % p, file=sys.stderr)
        print("", file=sys.stderr)
        print("Valid labels: %s" % ", ".join(LABELS), file=sys.stderr)
        return 2

    tally = {s: {lab: 0 for lab in LABELS} for s in SOURCES}
    for _lineno, qid, label in rows:
        source = key_questions[qid]["source"]
        if source not in tally:
            raise SystemExit("ERROR: key has unknown source %r for %s" % (source, qid))
        tally[source][label.lower()] += 1

    totals = {s: sum(tally[s].values()) for s in SOURCES}

    width = max(len(lab) for lab in LABELS) + 2
    header = "%-*s" % (width, "label") + "".join("%12s" % s for s in SOURCES)
    print("")
    print(header)
    print("-" * len(header))
    for lab in LABELS:
        print("%-*s" % (width, lab) + "".join("%12d" % tally[s][lab] for s in SOURCES))
    print("-" * len(header))
    print("%-*s" % (width, "total") + "".join("%12d" % totals[s] for s in SOURCES))

    def rate(s):
        if not totals[s]:
            return "n/a"
        return "%d/%d (%.0f%%)" % (
            tally[s]["good"],
            totals[s],
            100.0 * tally[s]["good"] / totals[s],
        )

    print("%-*s" % (width, "good rate") + "".join("%12s" % rate(s) for s in SOURCES))
    print("")

    ours_good = tally["ours"]["good"]
    deltas = {s: ours_good - tally[s]["good"] for s in ("nlm-pages", "nlm-book")}
    for s, d in deltas.items():
        print("ours good - %s good = %+d" % (s, d))
    print("")

    if all(d >= PASS_MARGIN for d in deltas.values()):
        print("VERDICT: PASS")
        print(
            "  Our set has at least %d more `good` questions than both "
            "NotebookLM sets." % PASS_MARGIN
        )
    elif any(d <= -PASS_MARGIN for d in deltas.values()):
        worse = [s for s, d in deltas.items() if d <= -PASS_MARGIN]
        print("VERDICT: FAILS PREMISE")
        print(
            "  Our set has at least %d fewer `good` questions than %s."
            % (PASS_MARGIN, " and ".join(worse))
        )
    else:
        losses = {lab: tally["ours"][lab] for lab in LABELS if lab != "good"}
        worst = max(losses, key=lambda lab: (losses[lab], lab))
        print("VERDICT: INCONCLUSIVE")
        print(
            "  The gap is within +/-%d of at least one NotebookLM set."
            % (PASS_MARGIN - 1)
        )
        if losses[worst]:
            print(
                "  Our set lost the most questions to `%s` (%d of %d)."
                % (worst, losses[worst], totals["ours"])
            )
        else:
            print("  Our set lost no questions to any non-good label.")

    print("")
    print("CAVEAT: n=%d per set only detects large differences." % max(totals.values()))
    print("        INCONCLUSIVE means NOT KNOWN, not `tied`. A real but modest")
    print("        difference would look exactly like this result.")
    return 0


# --- cli ---------------------------------------------------------------------


def main(argv=None):
    parser = argparse.ArgumentParser(
        prog="labelkit",
        description="Blind-labelling toolkit for the question-quality comparison.",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    b = sub.add_parser("build", help="merge, shuffle and emit the label sheet")
    b.add_argument(
        "--ours", nargs="+", required=True, metavar="RUN.JSON",
        help="one or more emitted run files from this repo's tool",
    )
    b.add_argument("--nlm-pages", required=True, metavar="TXT")
    b.add_argument("--nlm-book", required=True, metavar="TXT")
    b.add_argument(
        "--seed", type=int, required=True,
        help="integer seed; the same seed always produces the same sheet",
    )
    b.add_argument("--out", default=".", metavar="DIR", help="where to write sheet.csv and key.json")
    b.add_argument(
        "--include-failed", action="store_true",
        help="also label our questions that failed a gate (default: exclude)",
    )
    b.set_defaults(func=cmd_build)

    p = sub.add_parser("parse", help="validate a NotebookLM .txt on its own")
    p.add_argument("path", metavar="TXT")
    p.add_argument("--dry-run", action="store_true", help="print every question it found")
    p.set_defaults(func=cmd_parse)

    s = sub.add_parser("score", help="read the filled sheet plus the key and report")
    s.add_argument("--sheet", required=True, metavar="CSV")
    s.add_argument("--key", required=True, metavar="JSON")
    s.set_defaults(func=cmd_score)

    args = parser.parse_args(argv)
    try:
        return args.func(args)
    except ParseError as exc:
        print(exc.render(), file=sys.stderr)
        return 2


if __name__ == "__main__":
    if sys.stdout.encoding and sys.stdout.encoding.lower() not in ("utf-8", "utf8"):
        try:
            sys.stdout.reconfigure(encoding="utf-8")
            sys.stderr.reconfigure(encoding="utf-8")
        except Exception:
            pass
    sys.exit(main())
