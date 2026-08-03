# Verdict — in progress

The prototype asks one question:

> Does a chunk-grounded, gate-verified pipeline produce multiple-choice
> questions a human can actually interpret — and does it beat NotebookLM?

**Half the answer is in.** The pipeline runs end to end and the gates work. The
human half — reading 20 passing questions and comparing against NotebookLM on
the same file — has not been done, so the question is not closed.

Everything below was measured on this machine, not estimated.

## Setup

| | |
|---|---|
| GPU | RTX 4050 Laptop, 6141 MiB |
| CPU | Ryzen 7 8845HS, 8c/16t |
| RAM | 31.3 GB |
| Generator + judge | `scb10x/typhoon2.5-qwen3-4b` (2.5 GB, Q4_K_M) |
| Embeddings | `nomic-embed-text` |
| OCR | `scb10x/typhoon-ocr1.5-3b` (3.2 GB) |
| Rasteriser | poppler 25.07 `pdftoppm` |

## Runs

| document | pages | mode | generated | passed | pass rate |
|---|---|---|---|---|---|
| Thai course handout | 2–4 | text | 16 | 10 | **62%** |
| Thai course handout, repair on | 2–4 | text | 20 | 10 | 50% |
| Synthetic scan, `--force-calc` | 1–3 | ocr | 7 | 1 | 14% |
| Synthetic scan, `--force-calc`, calculator tool | 1–3 | ocr | 4 | 2 | **50%** |
| Thai course handout, calculator tool | 2–4 | text | 16 | 12 | **75%** |

### Hosted DeepSeek + full Thai textbook (2026-08-03)

The current Docling/graph pipeline was also run on the 254-page IPST ม.5
biology teacher book (347,577 runes, 392 chunks). The cached pass-1 result has
217 source-provenanced concepts, 240 evidenced edges, and 17 lessons; its cold
run used 13 bounded map calls plus one reduce call.

The repair call was tested before removal. It returned unchanged, duplicate, or
synonym-swapped distractors and its replacement contract was violated, so the
result was safely declined. That call was pure overhead. The implementation and
the `--repair` flag are now deleted.

The replacement is bounded rejection memory: at most four recent failed drafts
and their gate reasons ride on the next normal top-up prompt. It adds no request
of its own and tells the generator to ask something materially different rather
than edit its failed draft. A one-pair A/B run is stochastic and not enough to
claim a pass-rate improvement, but reading the output exposed the important
difference:

| mode | drafts | gate-passing | subject questions by manual read | API calls | model wall |
|---|---:|---:|---:|---:|---:|
| no rejection memory | 4 | 4 | 2/4 | 13 | 20.073s |
| rejection memory, before metadata ban | 7 | 4 | 4/4 | 21 | 30.911s |
| rejection memory + metadata prompt/gate | 4 | 4 | **4/4** | 14 | **17.827s** |

The no-memory run looked perfect to the automated gates, but two accepted
questions asked about the teacher guide's learning objectives and assessment
coverage instead of biology. Generation now explicitly excludes pedagogy and
document metadata, and a deterministic pre-judge gate rejects known Thai and
English teacher-guide phrases. The final four questions ask about digestive
tract order, the stages of food processing, where chemical digestion starts,
and why chewing helps digestion. All are source-grounded subject questions.

The final call breakdown was three generation calls (the first correctly
returned no questions for a metadata-only chunk), three calculator-tool calls,
four blind-judge calls, and four source-judge calls: **14 DeepSeek HTTP requests
total**. Summed request latency was 21.019s; parallel model wall time was
17.827s. A paid regression eval also confirms that `ถ่ายอุจจาระ` versus
`ขับถ่าย` is classified as an equivalent answer rather than silently accepted.

The last row is not a clean measurement of the calculator tool. The judge was
fixed in the same batch — its context window was too small, so long replies came
back as truncated JSON and killed whole runs — and that change alone plausibly
accounts for part of 62% → 75%. What it does establish is that the tool causes
no regression on prose questions, and that this lesson can fill a budget of 12
without hitting its ceiling.

The clean measurement of the calculator tool is the arithmetic column on the
scan: same document, same prompt, 5 failures to 0.

The 4B model set its own question budget at 1 for the first Thai lesson. A
budget of 1 makes the pass rate meaningless, so the runs above force a budget
to get a sample worth reading.

Top-up worked: when the chosen lesson ran dry, questions came from sibling
lessons in the same document, never from outside it, and the ceiling was
reported honestly rather than padded.

## What the gates caught

**Arithmetic — the headline result.** On the calculation run, gate 4 rejected 5
questions. Four were the model getting its own arithmetic wrong:

| expression the model wrote | Go computed | model claimed |
|---|---|---|
| `(1200*0.07)/3` | 28 | 36 |
| `480/(2000*0.06)` | 4 | 24 |
| `1000*(1+0.05)` | 1050 | 105 |
| `90*1588` | 142920 | 1587 |

Without this gate all four ship with a wrong answer key, and they look
completely plausible. This is the single strongest piece of evidence that the
design is right — and it argues the gate belongs in production regardless of
which model is used, because it costs nothing and catches everything.

The fifth rejection was our own bug: the model wrote `**` for exponentiation and
the evaluator only accepted `^`. Fixed.

**Quotes.** Gate 1 caught real fabrication: a model claiming the source read
`1200 * 0.07 = 0.07 baht` when it read `= 84 baht`, another returning an empty
quote, another transcribing "Creativity" as "Cretivity". It also produced a lot
of false rejections at first, all from extraction artefacts rather than model
error — see below.

## What had to be fixed to get here

Four defects, all found by running rather than by reading:

1. **`GetPlainText` is unusable.** It emits every PDF text run on its own line;
   "DLD-01" arrives as three lines. Rebuilt line assembly from `GetTextByRow`
   plus glyph coordinates.
2. **SARA AM loses its NIKHAHIT.** "คำจำกัดความ" extracts as "ค าจ ากัดความ".
   Repaired in `pdfx.repairThai`. Without it every Thai question fails gate 1.
3. **Gate 1 was comparing whitespace.** PDF extraction sprinkles spaces between
   Thai syllables; the model writes Thai without them. 12 of 15 correct quotes
   were being rejected over spacing alone. Comparison now ignores whitespace
   entirely while still requiring exact characters in exact order.
4. **Gate 1's length floor rejected numeric citations.** `1200 * 0.07 = 84 baht`
   is 22 runes and is stronger evidence than any 25-rune sentence. Quotes
   carrying two or more digits now have a lower floor.

## Decisions the measurements changed

**Extraction cannot be one extractor.** Both were tried on both documents:

| | Thai handout | arXiv two-column |
|---|---|---|
| `ledongthuc/pdf` (Go) | correct after repairs | **all glyph coordinates are zero** — every word runs together |
| poppler `pdftotext` | **drops the NIKHAHIT with no trace** — "คำ" becomes "คา", a different, valid-looking word | correct |

Neither wins. `--extract=auto` now picks per document: poppler when the Go path
got no geometry, the Go path when the document is substantially Thai. The
reason Thai goes to the Go path is not layout quality — poppler's layout is
better — it is that the Go path's damage leaves a space where the missing mark
was, and a space is recoverable. Poppler's damage is silent.

Also worth knowing: Git for Windows ships an Xpdf 4.00 binary called
`pdftotext` that sits ahead of poppler on PATH and is worse at Thai. `pdfx`
looks for real poppler installs before falling back to PATH.

**Correcting the model afterwards fails. Stopping it from doing arithmetic at
all works.** These sound like the same idea and are not, and the difference is
the largest single improvement measured here.

*Repair loop (removed)* — a gate rejects a question, we hand it back with the exact
discrepancy and ask for a fix. Measured on both documents: **4 sent back, 0 came
back clean, twice.** Recognising your own error is a harder task than making it,
and a 4B model cannot do it even when told precisely what is wrong. DeepSeek
later failed the same test by returning unchanged, duplicate, or synonym-swapped
distractors. The code and `--repair` flag were deleted. Rejected patterns now
ride on the next generation prompt, which costs no extra request.

*Calculator tool* — before writing anything, the model calls `calc(expression)`
and Go answers. It never has to compute. This is not the same request: it is
"don't make the mistake", not "find your mistake". On the same calculation run:

| | generated | passed | pass rate | arithmetic failures |
|---|---|---|---|---|
| no tool | 7 | 1 | 14% | 5 |
| repair loop | 11 | 1 | 9% | 6 |
| **calculator tool** | 4 | 2 | **50%** | **0** |

Arithmetic failures went to zero. On by default, `--calc-tool=false` to disable.

Two facts this depended on, both verified rather than assumed:

- The model does support tools. `ollama show` reports `completion, tools`; the
  model's page on ollama.com does not list the capability and is wrong.
- **Tools and a `format` JSON schema cannot be used in the same request.** With
  both set, the grammar wins: the model never emits a tool call and quietly
  answers from its own head. Generation therefore runs as two turns — tools
  first with no schema, then the schema-constrained write-up with the verified
  numbers supplied as given facts.

The arithmetic gate stays either way. It costs nothing, and an expression stored
in the database can be re-verified in five years, which a tool call that already
happened cannot.

## Reading the questions changed the number

14 questions that passed all four original gates were read by hand. Five were
outright broken: a stem truncated mid-word with no question in it, a correct
choice that was the stem repeated verbatim, a corrupted token inside a Thai word
("สร้างสรningerช่วย"), two questions with two defensible answers, and a
calculation question whose four options each restated the scenario so the answer
could be found by matching numbers. **By eye, 3 of 14 were good.** The blind
judge had called every one of them interpretable.

So seven deterministic checks were added — no model call, all of them string
work: stem must contain a question word, no choice may repeat the stem, no
mojibake, choices must be distinct, choice lengths must be comparable, no
"according to the passage", and no set of choices that all restate the question.
Replayed over those same 14, they reject exactly the five broken ones plus two
using forbidden self-reference, and leave the seven reasonable ones alone.

Getting there required backing out one bad rule of my own: "all choices share a
long substring" flagged two *well-written* questions whose Thai options all
began "ความสามารถในการ". That is parallel construction, which is good practice.
The discriminating property is choices echoing the **stem**, not resembling each
other.

## The embedding model could not read Thai at all

The duplicate-question check rejected 7 of 16 on its first live run, which looked
like a great catch until the reasons were read: it was scoring completely
unrelated Thai questions at 95% similarity. Measured directly, one pair per row:

| pair | nomic-embed-text | bge-m3 |
|---|---|---|
| identical wording | 1.0000 | 1.0000 |
| same question, reworded | 1.0000 | 0.9527 |
| different questions, same chapter | **1.0000** | 0.5953 |
| different questions, same chapter (2) | **1.0000** | 0.6388 |
| unrelated topics | 0.5311 | 0.2925 |
| English control, different | 0.4249 | 0.3251 |
| **usable gap** | **0.0000** | **+0.3140** |

`nomic-embed-text` returns 1.0000 for every pair of Thai sentences from the same
chapter regardless of meaning. No threshold can work against that.

This mattered well beyond the duplicate gate: the same embedder ranks chunks
before generation and backs `--scope`. **Chunk ranking on Thai had been random
this whole time.** Default is now `bge-m3`.

## Where the time goes — it is the call count, not the GPU

| call | count | total | share | out tok | tok/s |
|---|---|---|---|---|---|
| generate | 8 | 2m15s | 42% | 6978 | 53.4 |
| judge/source | 8 | 1m29s | 28% | 1093 | 41.2 |
| judge/blind | 9 | 55s | 17% | 1738 | 54.3 |
| calc-tool | 8 | 40s | 13% | 16 | 75.9 |
| **total** | **33** | **5m19s** | | | |

53 tok/s is what a 4B Q4 does when it is entirely in VRAM; a model spilling to
system RAM runs in the low single digits. There was no model-load time either.
So 6 GB is not the constraint — the two judges are, at 45% of wall clock
combined.

Running the structural checks first and returning early already halves that:
16 questions produced only 8–9 judge pairs instead of 16, because the broken
ones never reach a model.

## Still open

1. **The human half.** Nobody has read 20 passing questions and judged them, and
   nobody has run the same PDF through NotebookLM. Until both happen the
   headline question is unanswered — 62% of questions passing four automated
   gates is not the same as 62% being good.
2. **The judge is the generator.** Same 4B model grades its own work.
   `single_defensible` and `answerable_blind` should be treated as advisory
   until a second model checks them.
3. **The OCR numbers are from a synthetic scan.** `samples/scanned-textbook.pdf`
   is generated, with a bitmap font. typhoon-ocr read it but truncated line ends
   and misread 1500 as 1588 — that error then propagated into a question. A real
   scan of a real book would behave differently and has not been tried.
4. **One Thai document, three pages, one lesson.** The sample is small.
