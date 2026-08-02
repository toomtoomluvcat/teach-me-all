# Handoff — exam-quality prototype

Last updated 2026-08-03. Supersedes the earlier copy in the OS temp directory;
this file is canonical, delete that one.

Repo: `E:\contribute\teach-me-all`. Branch `prototype/exam-quality`, five commits
ahead of `main`, with `origin/prototype/exam-quality` at `339827d`. Work done
after that commit is uncommitted; see "Uncommitted work" below.

## Read these first, in this order

Do not re-derive any of it from the code.

| what | where |
|---|---|
| What the prototype is, how to run it, the gates | `backend/prototype-exam-quality/README.md` |
| **Every measured number and what is still unproven** | `backend/prototype-exam-quality/VERDICT.md` |
| Why the design is shaped this way (provider limits, RAG, question quality) | `docs/research/ai-exam-generation.md` |
| Exact API field names (Ollama, ledongthuc/pdf, poppler, expr) | `docs/research/ollama-and-go-libs-api-spec.md` |
| Test PDFs, what edge case each covers, how to re-download | `samples/README.md` |
| The three findings that shaped the first cut | `git show 9a0d935` |

## The product, in one paragraph

Upload PDFs. AI pass 1 splits them into a course of lessons. User picks a
lesson. AI pass 2 writes multiple-choice questions grounded in that lesson's
text. Questions are persisted and reused, never regenerated at runtime. The
owner's stated fear is questions that are vague or uninterpretable — "NotebookLM
ออกข้อสอบกาก" — so quality is the point, not quantity. Calendar/course
scheduling is a later phase, out of scope so far.

## Decisions already settled with the owner

From a long grilling session plus later course corrections. Do not reopen
without a reason; several are recorded nowhere else.

- The prototype answers **one** question: is a generated question interpretable,
  and does it beat NotebookLM. Not schema, not UI.
- Quality is judged by an automatic gate **and** by the owner's eyes. Neither
  alone.
- Source text is persisted as documents + chunks with page and offset so a
  question can cite a chunk. Not a text blob on the lesson; not re-sending the
  PDF each time.
- A question carries stem, 4 choices, explanation, citation, and metadata.
- The schema must support kinds beyond MCQ later: `questions.kind` + normalised
  `choices` for choice-shaped kinds + `answer_spec JSONB` for the rest. The
  prototype only generates `mcq_single`.
- Every generated question is kept, passing or failing, with a status and a gate
  report. Only passing ones are served.
- When a lesson cannot fill its budget, top up from **the user's own other
  lessons**, then stop and say so. **No web search.** The owner floated it; it
  was argued against because it breaks the citation gate and tests the learner on
  material they never read. The owner accepted that.
- MVP: the model decides how many questions a lesson supports.
- Arithmetic lives in a `calculation.expression` field that Go evaluates, not a
  stored tool-call transcript, because a stored expression is re-verifiable
  years later.
- A proposed production schema — `documents`, `document_pages`, `chunk_sets`,
  `chunks` with `vector(1024)`, and the `questions` columns — was written out in
  conversation but **not yet committed as a migration**. Rebuild it from
  "Storage design" below.

## Where the work actually stands

Six gates now, not four. Two are model-backed, four are pure Go:

| gate | decided by |
|---|---|
| `well_formed` — nine structural checks | Go |
| `quote_verbatim` | Go |
| `arithmetic` | Go |
| `not_a_duplicate` — embedding similarity vs accepted questions | Go + embedder |
| `answerable_blind` — is it clear what is being asked | model |
| `single_defensible` — does exactly one choice hold up | model |

Deterministic checks run first and short-circuit: a question Go has already
rejected never reaches a judge.

Measured pass rates, all on `scb10x/typhoon2.5-qwen3-4b`:

| document | generated | passed | rate |
|---|---|---|---|
| Thai handout, four gates | 16 | 10 | 62% |
| Thai handout, six gates | 16 | 3 | 19% |
| Biology ch. 8, six gates | 22 | 10 | 45% |

The rate fell because the checks got honest, not because anything regressed.

## The one defect that keeps recurring

**A distractor that happens to be a true statement.** Observed six times across
two documents and two languages. Examples from the biology run: "Photosynthesis
converts sunlight into chemical energy" offered as a wrong answer; "Photosynthesis
requires sunlight and aerobic respiration does not" offered as a wrong answer.
Both true. The `single_defensible` judge passed all of them.

No Go rule can catch this — it requires knowing whether a sentence is true of the
material. **This is the only remaining argument for a larger model**, and it
should be spent on the `judge/source` call specifically, not on generation.

Reading the 10 passing biology questions by hand: 3 genuinely good, 3 with a true
distractor, 4 weak (off-topic distractors, or answerable without reading). So 45%
still overstates it.

## Uncommitted work since `339827d`

Everything below is in the working tree, builds, and `go vet` is clean. It is not
committed.

- `pdfx/docling.go`, `docling_helper.py`, `auto.go`, and tests — local Docling
  standard-pipeline extraction using pypdfium2. `auto` is Docling-only and uses
  EasyOCR `th,en` by default. Failure is surfaced; there is no text-only or LLM
  OCR fallback. Docling's Markdown serializer omits empty physical pages, so the
  helper now aligns serialized sections back to Docling page numbers and keeps
  empty page files/sections instead of rejecting a valid document.
- `pdfx/bundle.go` and tests — durable extraction bundle containing a manifest,
  Docling JSON, combined/per-page Markdown and plain text, source SHA-256, and
  figure-level crops referenced from Markdown. Fresh runs remove stale managed
  assets; full-page screenshots are no longer generated.
- `main.go` and tests — Docling flags, `--extract=docling`, v3 cache envelopes
  that preserve the prepared Markdown/asset graph, and bundle creation on every
  `--extract-only` run including cache hits. Extraction reports a heartbeat with
  elapsed time while Docling runs. The preview only prints keyboard actions when
  it is actually waiting; quit is line-based (`q` then Enter).
- Hosted pass-1 mapping no longer sends a whole textbook in one request. Gemini
  and DeepSeek use bounded 32-chunk/36k-rune batches, parallelised by
  `--parallel`; malformed JSON recursively splits only the failed batch and
  terminal errors cancel pending work. The 392-chunk Thai biology benchmark
  plans 13 map calls plus one reduce call. DeepSeek usage tokens are now included
  in the cumulative call report.
- The real DeepSeek rerun of that 392-chunk benchmark passed: 13 map calls plus
  one reduce call, about 1m wall clock at `--parallel 4`, 132,003 input tokens
  and 18,485 output tokens. It produced 273 topics and 43 lessons. The flat
  topic-to-outline reduce left 70 chunks unassigned; this is concrete evidence
  for the next experiment being a provenance-preserving evidence graph
  (`concept/edge -> chunk_ids/assets`), evaluated A/B rather than adopted as a
  database architecture up front.
- `setup-docling.ps1` — reproducible pinned project-local runtime installation.
- `backend/prototype-exam-quality/README.md` — extraction bundle layout and the
  reusable `protoexam.exe` workflow.

## Findings that will not be obvious from the code

- **`nomic-embed-text` cannot read Thai.** Cosine 1.0000 for every pair of Thai
  sentences from the same chapter, whether the same question or not. Gap between
  "duplicate" and "different" is zero. `bge-m3` gives 0.95 vs 0.60, gap 0.31.
  This also means chunk ranking and `--scope` were random on Thai before the
  switch. bge-m3 is 1024-dim, nomic is 768 — changing embedder invalidates every
  stored vector.
- **The discarded text-layer extractors failed in different ways.** The Go,
  Poppler, and Typhoon OCR paths were useful experiments but are no longer in
  runtime code. Docling is now the only PDF extraction contract so every bundle
  has the same Markdown/table/figure representation.
- **Tools and a `format` JSON schema are mutually exclusive in Ollama.** With
  both set the grammar wins, no tool call is emitted, and the model quietly
  answers from its own head. Generation is two turns because of this.
- **DeepSeek JSON mode is syntax-only.** On the first live run it omitted map
  results and returned legacy question fields (`question`, `correct_index`, and
  string choices); explicit shape instructions fixed the next run. The second
  live run mapped 13 chunks in one `outline/map` call, reduced in one call, and
  completed the selected lesson with 6 total provider calls (one generation
  retry was needed). The fallback remains for regressions in provider output.
- **Correcting a 4B model does not work; preventing the error does.** The repair
  loop repaired 0 of 4, twice. The calculator tool took arithmetic failures from
  5 to 0 on the same document.
- **Model swapping, not the GPU, is the biggest time sink.** A biology run spent
  4m39s of 11m54s on model loading, because the 4B generator and bge-m3 cannot
  both stay resident once KV cache is counted. Generation itself runs at 35–55
  tok/s, which is healthy and means 6 GB is not the bottleneck.
- **The timing report's `share` column was wrong.** It now divides by measured
  model-call wall clock; live reruns still need to confirm the resulting numbers.
- **A cached biology rerun after batching** generated 21 questions and passed 10
  (47.6%), versus the prior 22/10 (45.5%). Its report showed 7m55s wall clock and
  4m36s loading, versus the prior 11m54s and 4m39s. The run happened before the
  new `embed` bucket, so it suggests a wall-clock improvement but does not yet
  isolate embedding time.
- **The immediate follow-up with the `embed` bucket** made 9 embedding calls
  taking 3.007s (1% of call time), 52 total calls, 5m23s wall clock, and 3m15s
  model loading; it generated 19 and passed 10 (52.6%). Because it followed the
  prior run while models were still warm, this is instrumentation evidence, not
  a clean cold-cache A/B measurement.

## Storage design, as agreed in conversation

Extraction produces exactly two shapes: `Page{Number, Text}` and
`Chunk{ID, Page, StartOff, EndOff, Text, LessonID}`. Offsets are **rune** offsets
within a page, never bytes and never document-global. Chunks target 1200 runes
with 150 overlap and never cross a page boundary, so a cited page number is
always exactly true. Real sizes: 19 biology pages → 46 chunks; 3 Thai pages → 6.

Tables to create: `documents` (with `sha256` and a **pinned `extract_mode`** —
re-extracting with a different extractor shifts every offset and breaks every
stored citation), `document_pages` (`markdown` for reading/model context and
derived `plain_text` for search/quote offsets, so re-chunking never requires
re-running OCR), `document_assets` (page/figure image, MIME type, storage key,
page number and later bounding box/alt text), `chunk_sets` (versioned, so
changing chunk size does not orphan questions that reference old chunks),
`chunks` (page_no, ordinal, start_off, end_off, text, `vector(1024)`, HNSW
index), and later `question_assets` for questions that require an image.

Three decisions that are painful to reverse:

1. Store `chunks.text` denormalised rather than slicing `document_pages` at read
   time. A stored question must stay verifiable after the source file is deleted.
2. Never re-chunk in place. New chunking run → new `chunk_set`.
3. Offsets are runes. Thai is 3 bytes per character; a byte offset anywhere cuts
   characters in half. Postgres `substring(text FROM n FOR m)` counts characters
   and lines up; `substr()` on `bytea` does not.

## OCR representation decision now settled

Keep both representations. Markdown is canonical for the reading UI and model
context; derived plain text is canonical for search, chunk offsets and exact
quote checks. Images are separate figure assets referenced by Markdown, never
base64 inside stored content. Docling now extracts figure-level crops rather
than keeping every PDF page as a PNG. Image-dependent questions must attach the
relevant source asset or be rejected; an AI-generated description alone is not
sufficient evidence.

## Environment on this machine

- Ollama 0.32.5 at `%LOCALAPPDATA%\Programs\Ollama`, **not on PATH**. Running
  with `OLLAMA_NUM_PARALLEL=4` and `OLLAMA_MAX_LOADED_MODELS=2` (set as user env
  vars; a restart is needed for changes to take).
- Generation models: `scb10x/typhoon2.5-qwen3-4b`, `bge-m3`, and
  `nomic-embed-text`. PDF extraction does not use these models.
- Docling 2.117.0, RapidOCR 3.9.2, EasyOCR 1.7.2 and ONNX Runtime 1.28.0 live in
  repo-level `.scratch/docling-venv`. `setup-docling.ps1` recreates it. This path
  is entirely local and makes zero Gemini/DeepSeek/generative-LLM calls.
- Full fresh extraction of `samples/thai-highschool-biology-ipst.pdf` passed on
  2026-08-03: 254 physical pages, 254 Markdown page files, 253 combined-document
  separators, 242 figure crops and 347,577 runes. Pages 2 and 234 are valid empty
  Markdown pages. Runtime was about 8m32s; the next cache hit completed in about
  1.1s without running Docling again.
- RTX 4050 Laptop 6 GB, Ryzen 7 8845HS, 31 GB RAM but only ~8 GB free during the
  session, which is why the 19 GB `typhoon2.5-qwen3-30b-a3b` was never tried.
- PowerShell 5.1: no `&&`. Build once with `go build -o protoexam.exe .`.

## Known defects in the existing production code

Found by reading, not fixed, filed nowhere else:

- `models.Question.IsCorrect` exists in Go but in no migration. Repositories use
  `Find` → `SELECT *`, so there is no error — every row serialises
  `"isCorrect": false` forever. `is_correct` also belongs on `choices`, which has
  no such column, so the answer key is currently unrepresentable.
- `dto.ExamWithQuestions.Title` is tagged `json:"content"`.
- `questions.content` and `choices.content` are `VARCHAR(100)`.
- Five of six migrations have no `.down.sql`; no FK has an index.
- `handlers.New` calls `NewQuestionRepository` to build a handler.

## What to do next, in the order it makes sense

1. **Close the prototype.** The owner still has not compared against NotebookLM
   on the same file. Keep asking before anyone builds on the numbers.
2. Replay the six known true-distractor questions through `judge/source` only,
   once with local 4B and once with DeepSeek. The DeepSeek smoke run proved the
   wiring, not that the larger judge catches the recurring defect.
3. Obtain a genuine Thai camera/flatbed scan. The new official สสวท. benchmark
   is digital (Biology M.5 book 4); pages 60-62 prove Thai text, table, page
   breaks and figure crops, but do not prove robustness to skew/shadows/blur.
   If image-dependent questions are in MVP scope, add `question_assets`;
   otherwise forbid those questions explicitly.
4. **If a clean performance claim matters, do one cold-cache rerun** after
   unloading both models. The instrumentation now shows the batching path itself
   is only 9 embed calls / 3.007s on this workload; the remaining load cost is
   generator/judge model swapping.
5. Commit the uncommitted extraction-bundle work.

## Suggested skills

- **`mattpocock-skills:grilling`** — before production code. The next fork (how
  much of `examgen/` to lift, the real migration, in-request vs job queue) has
  several answers and they are the owner's to pick.
- **`mattpocock-skills:domain-modeling`** then **`mattpocock-skills:codebase-design`**
  — for the schema above against the existing `internal/{models,dto,repository,handlers}`
  layering.
- **`mattpocock-skills:tdd`** — production code, unlike the prototype, should have
  tests. `examgen`'s gates and arithmetic evaluator are pure functions and are the
  obvious first targets.
- **`mattpocock-skills:prototype`** — read rule 6 before deleting anything.
- **`mattpocock-skills:diagnosing-bugs`** — if extraction misbehaves on a new PDF.
  Every extraction defect so far was found by looking at extracted text, never by
  reading code. `--extract-only` does that and needs no Ollama.

## Traps

- `.scratch/` caches extraction and the pass-1 outline per (file, mode, page
  range). Extraction uses `pages-v3.json` so caches from removed fallback paths
  cannot satisfy Docling-only runs. Edit a prompt, rerun, and you get the cached outline. Pass
  `--fresh`.
- Without `--budget` the model often sets its own budget to 1, and a 1/1 pass
  rate measures nothing.
- Without `--pages`, pass 1 fires one model call per chunk across the whole
  document. On a textbook that is hours.
- `--repair` is off because it measured worthless on a 4B model. That is a fact
  about 4B, not about the idea. Re-measure on a frontier model.
- Changing `--embed-model` invalidates every stored vector; the dimension changes
  too (bge-m3 1024, nomic 768).
- Re-running a structural check over old `.scratch/*/run-*.json` files is the
  cheapest way to test a new rule — `examgen.CheckWellFormed` is exported for
  exactly that, and it caught a false positive of mine that would otherwise have
  shipped.
