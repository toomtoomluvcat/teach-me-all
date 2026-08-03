# labelkit — blind labelling for the question-quality comparison

Three sets of questions (ours, NotebookLM given only the lesson pages,
NotebookLM given the whole book) get merged, shuffled and stripped down to
stem + four choices. A human labels all of them without knowing which tool
produced which. Then the scorer puts the key back on and reports.

Standard library only, no network. Run everything with `py` from
`backend/prototype-exam-quality/`.

## The three commands, in order

### 1. Check each NotebookLM paste

Do this **before** building the sheet. The parser refuses to guess, so a paste
it cannot read cleanly is an error you fix by hand, not something it silently
patches up.

```powershell
py tools\labelkit.py parse .\nlm-pages.txt --dry-run
py tools\labelkit.py parse .\nlm-book.txt  --dry-run
```

`--dry-run` prints every question it found, numbered, with its four choices and
a `*` on the stated answer. Read it. If a stem or a choice looks wrong, the
`.txt` is wrong — fix the `.txt`, not the parser.

### 2. Build the sheet

```powershell
py tools\labelkit.py build `
  --ours .scratch\<hash>\run-lesson-a.json .scratch\<hash>\run-lesson-b.json `
  --nlm-pages .\nlm-pages.txt `
  --nlm-book  .\nlm-book.txt `
  --seed 20260803 `
  --out .\labelling
```

Writes `labelling\sheet.csv` and `labelling\key.json`.

- `--seed` is required and is the only source of randomness. The same seed and
  the same inputs reproduce the sheet byte for byte.
- `--ours` takes one or more run files, because one run file covers one lesson.
  `original_index` in the key counts 1..N across them in the order given.
- Questions that failed a gate are excluded by default (that is what the tool
  would actually ship). `--include-failed` keeps them.
- `sheet.csv` is UTF-8 **with BOM** so Excel on Windows renders Thai correctly.

Now open `sheet.csv` and fill the `label` column. One of:

| label | means |
| --- | --- |
| `good` | a question worth asking |
| `true_distractor` | a wrong option that is obviously wrong; no real choice |
| `off_topic` | not about the lesson |
| `no_reading_needed` | answerable from general knowledge without the source |
| `teacher_guide` | asks about teacher-facing apparatus, not subject matter |
| `ambiguous` | more than one defensible answer, or the stem is unclear |

Case does not matter. Every row must be labelled.

**Do not open `key.json` until the sheet is filled.** Nothing in `sheet.csv`
reveals the answer, the explanation, the source quote, or which tool wrote the
question — that is the whole point.

### 3. Score

```powershell
py tools\labelkit.py score --sheet .\labelling\sheet.csv --key .\labelling\key.json
```

Prints a per-source count of every label, the `good` rate, and the pre-agreed
verdict:

- **PASS** — our `good` count is at least 4 higher than *both* NotebookLM sets.
- **FAILS PREMISE** — our `good` count is at least 4 lower than *either* set.
- **INCONCLUSIVE** — anything else, plus the label our set lost the most
  questions to.

`score` exits non-zero and names the offending rows if a label is missing,
misspelled, duplicated, or not in the key. It reports nothing until the sheet
is clean.

## What INCONCLUSIVE means

n=15 per set only detects large differences. INCONCLUSIVE means **not known**.
It does not mean the tools are tied — a real but modest quality difference
produces exactly this result. The scorer prints this caveat every time.

## NotebookLM formats the parser accepts

The export shape was unknown when this was written, so the parser takes the
common shapes and rejects everything else loudly:

- Option labels `A B C D`, `a b c d`, `ก ข ค ง`, or `1 2 3 4`, followed by
  `.` `)` `:` `-`, optionally wrapped in parentheses. It picks whichever
  alphabet yields the most complete four-in-a-row groups, so a file numbered
  `1.` `2.` `3.` with options `A.`–`D.` is never misread as numeric options.
- Question lines numbered bare (`3.`), or prefixed with `Q`, `Question`, `ข้อ`,
  `ข้อที่`, `คำถาม`. A stem wrapped over several lines is joined.
- Answer lines are optional: `Answer:`, `Ans:`, `Correct answer:`, `Key:`,
  `เฉลย:`, `คำตอบ:` / `คำตอบคือ`, with the choice given in any of the four
  alphabets.
- Markdown decoration is stripped: `**bold**`, `__bold__`, backticks, bullets
  (`-` `*` `+` `•`), blockquote `>`, non-breaking and zero-width spaces.

It fails, names the line, prints the raw block and exits 2 when:

- no complete group of four options exists in any alphabet;
- a question has three or five options;
- a block has more than one line of question text and the stem is ambiguous;
- there is a heading or commentary before a question (delete it — NotebookLM
  likes to open with "Here is a set of practice questions…");
- there is trailing text after the last question;
- an answer line names a choice outside A–D / ก–ง / 1–4, or a question has two.

It never pads, truncates or skips a block. Silently mis-reading 15 questions
would destroy the measurement.

## Self-test

```powershell
py tools\selftest.py
```

Runs the whole flow against `tools\testdata\` in a temp directory: both
malformed fixtures really do exit non-zero, the same seed really does reproduce
the sheet, a different seed changes it, the sheet really does carry no answer
and no provenance, and a PASS verdict comes out of a fully-labelled sheet. The
INCONCLUSIVE and FAILS PREMISE branches were checked by hand, not by this
script.
