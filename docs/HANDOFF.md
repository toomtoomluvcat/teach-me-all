# Handoff — exam-quality prototype

Last updated 2026-08-03. Supersedes the earlier copy in the OS temp directory;
this file is canonical, delete that one.

Repo: `E:\contribute\teach-me-all`. Branch `prototype/exam-quality`, ten commits
ahead of `main`. Current HEAD is `1d99b8e`; `origin/prototype/exam-quality` is
`f929434`, so the branch has one local commit not pushed. The source tree was
clean before this handoff update; this `docs/HANDOFF.md` edit is uncommitted.

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

Six gates now, not four. Two are model-backed and four are deterministic or
locally reproducible:

| gate | decided by |
|---|---|
| `well_formed` — ten structural checks, including teacher-guide metadata | Go |
| `quote_verbatim` | Go |
| `arithmetic` | Go |
| `not_a_duplicate` — embedding similarity vs accepted questions | Go + embedder |
| `answerable_blind` — is it clear what is being asked | model |
| `single_defensible` — does exactly one choice hold up | model |

Deterministic checks run first and short-circuit: a question Go has already
rejected never reaches a judge.

Pass 1 is now a provenance-preserving evidence graph, not a flat topic list.
The cached full-book result for the 254-page Thai IPST biology teacher book has
217 concepts, 240 evidenced `co_occurs`/`follows` edges, and 17 lessons. Every
concept retains source chunk/page provenance and generation sees only concepts
evidenced by its current chunk.

Failed questions are **not repaired**. The repair implementation, prompts,
tests, counters, UI labels and `--repair` flag were deleted. The newest four
failed drafts are kept as compact rejection memory and appended to the next
normal top-up generation prompt. This adds no dedicated provider request and
asks for a materially different question rather than a paraphrased fix.

The generator is also explicitly forbidden from asking about learning
objectives, assessment rubrics/guidelines, classroom activities, videos,
pedagogy or chapter numbering. A deterministic pre-judge check catches known
Thai and English teacher-guide phrases if the model ignores the prompt.

Teacher-guide material is now also removed one stage earlier, at graph
compilation, and **the model decides it, not a phrase list**. The pass-1 map
step returns `{title, kind}` per topic, where kind is `content`, `apparatus`
(answer keys, assessment rubrics, scoring guides, test banks, learning
objectives, lesson plans, teaching hours) or `non_content` (page furniture,
replacing the old `NON_CONTENT` sentinel). `BuildEvidenceGraph` admits only
`content`, so a chunk whose topics were all apparatus never joins a lesson.

This costs no extra provider call: the map step was already reading every chunk.
A Thai/English phrase list was written first, measured, and then deleted — it
scored 36 of 217 concepts on the cached biology graph with no false positives,
but it only ever knew the wording of the one publisher it had been read from,
and the classification is the map step's job anyway.

Two deliberate rules the prompt spells out, because they are the pairs that
actually confuse a classifier: a page of answers to chapter exercises is
apparatus even though every answer is about biology, and a laboratory activity
is content even though a teacher runs it. Teacher-knowledge sidebars
(`ความรู้เพิ่มเติมสำหรับครู: Osmolarity`) are content too.

`examgen.Topic.UnmarshalJSON` accepts a bare string as well as the object, and a
topic with a missing or unknown kind is treated as content — wrongly deleting
material costs more than letting one rubric reach the question-level gates,
which still ban teacher-guide stems.

Measured pass rates, all on `scb10x/typhoon2.5-qwen3-4b`:

| document | generated | passed | rate |
|---|---|---|---|
| Thai handout, four gates | 16 | 10 | 62% |
| Thai handout, six gates | 16 | 3 | 19% |
| Biology ch. 8, six gates | 22 | 10 | 45% |

The rate fell because the checks got honest, not because anything regressed.

Latest hosted DeepSeek measurement on lesson 3 (human digestion), budget 4:

| mode | drafts | gate-passing | actual subject questions by manual read | API calls | model wall |
|---|---:|---:|---:|---:|---:|
| no rejection memory | 4 | 4 | 2/4 | 13 | 20.073s |
| rejection memory, before metadata ban | 7 | 4 | 4/4 | 21 | 30.911s |
| rejection memory + metadata prompt/gate | 4 | 4 | **4/4** | **14** | **17.827s** |

The final 14 calls were `generate` 3, `calc-tool` 3, `judge/blind` 4 and
`judge/source` 4. The first generation call correctly returned no questions for
a metadata-only chunk. Summed request latency was 21.019s; model wall was lower
because judge calls run in parallel. The four accepted questions were read by
hand and ask about digestive-tract order, stages of food processing, where
chemical digestion begins, and why chewing helps digestion.

## The quality defect that still deserves a larger eval

**A distractor that happens to be a true statement.** Observed six times across
two documents and two languages. Examples from the biology run: "Photosynthesis
converts sunlight into chemical energy" offered as a wrong answer; "Photosynthesis
requires sunlight and aerobic respiration does not" offered as a wrong answer.
Both true. The `single_defensible` judge passed all of them.

No Go rule can catch this — it requires knowing whether a sentence is true of
the material. A paid DeepSeek regression now confirms one important Thai case:
`ถ่ายอุจจาระ` versus `ขับถ่าย` is classified as equivalent rather than silently
accepted. That proves the source-judge wiring and one semantic case, but the six
historical true-distractor questions have not all been replayed as a proper
local-4B-vs-DeepSeek eval. If model spend is limited, spend it on
`judge/source`, not generation.

Reading the 10 passing biology questions by hand: 3 genuinely good, 3 with a true
distractor, 4 weak (off-topic distractors, or answerable without reading). So 45%
still overstates it.

## Recent committed work

The extraction, hosted batching, graph generation, repair removal and metadata
gate are committed through `1d99b8e`. The important commits after the old
handoff baseline are:

- `3919792` — remove legacy PDF extractors and make Docling the runtime path.
- `9053549` — align Docling serialization to physical pages and preserve empty
  page files.
- `9893339` — bounded hosted-provider pass-1 batching and provider error
  handling.
- `f929434` — evidence-graph generation and semantic distractor hardening.
- `1d99b8e` — remove the unsuccessful repair loop, add bounded rejection memory,
  block teacher-guide metadata, record the live DeepSeek A/B, and rebuild
  `protoexam.exe`.

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
- The real DeepSeek rerun of that 392-chunk benchmark passed. The earlier flat
  map produced 273 topics and 43 lessons with 70 chunks unassigned; that result
  motivated the evidence graph now in the code. The current cached graph has
  217 source-provenanced concepts, 240 edges and 17 lessons.
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
- **Correcting a model afterwards does not work; preventing the error does.**
  The local 4B repair loop repaired 0 of 4 twice. DeepSeek later returned
  unchanged, duplicate, or synonym-swapped distractors and violated the focused
  replacement contract. Repair is therefore gone, not merely disabled. The
  calculator tool still took arithmetic failures from 5 to 0 on the same local
  document.
- **Automatic gate pass rate alone still lies.** In the no-memory DeepSeek run,
  all four drafts passed every gate, but two asked about the teacher guide's
  learning objectives/assessment rather than biology. Manual reading caused the
  new metadata prompt and deterministic check.
- **`calc-tool` currently costs one provider call per generation call even on a
  prose-only biology lesson.** The final run spent 3 of 14 calls there and
  produced no calculation questions. `--calc-tool=false` is the immediate cheap
  mode for a known non-mathematical source; a cleaner future optimisation is to
  invoke the tool turn only for chunks/concepts likely to require arithmetic.
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

1. **Close the human-quality half of the prototype.** Read and label at least 20
   passing questions, then compare the same lesson/PDF with NotebookLM. The
   latest four were read and are acceptable, but four is not the agreed sample.
2. Replay the six historical true-distractor questions through
   `judge/source`, once with local 4B and once with DeepSeek. The current paid
   regression proves one Thai paraphrase case, not the whole recurring defect.
3. **Measure the topic classifier on one real pass 1.** The code is written and
   tested; whether the model actually classifies well is unmeasured, and it is
   the whole mechanism now that the phrase list is gone. The biology outline
   cache was invalidated (`outline-v2` → `outline-v3`), so the next run of that
   book pays for pass 1 again: 13 map calls plus one reduce on DeepSeek. Do it
   once, then compare the concept list against the 36 the deleted phrase list
   found — those are recorded in `git show e2aec77` and are a usable answer key.
   Watch two things: a 4B local model may classify worse than a hosted one, and a
   model that marks everything `content` degrades silently back to the old
   behaviour rather than failing.
4. For known prose subjects, measure `--calc-tool=false`. It should remove the
   three calculator-tool calls seen in the 14-call biology run; keep the
   arithmetic gate regardless. Do not make this the global default without a
   routing rule because quantitative science/math passages need the tool.
5. Obtain a genuine Thai camera/flatbed scan. The official สสวท. benchmark
   is digital (Biology M.5 book 4); pages 60-62 prove Thai text, table, page
   breaks and figure crops, but do not prove robustness to skew/shadows/blur.
   If image-dependent questions are in MVP scope, add `question_assets`;
   otherwise forbid those questions explicitly.
6. **If a clean performance claim matters, do one cold-cache rerun** after
   unloading both models. The instrumentation now shows the batching path itself
   is only 9 embed calls / 3.007s on this workload; the remaining load cost is
   generator/judge model swapping.
7. Only after the quality comparison, decide how much of `examgen/`, `pdfx/`
   and `llm/` to lift into the production backend and write the agreed storage
   migration. The prototype executable is reusable, but it is not production
   architecture by itself.

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
  cannot satisfy Docling-only runs. Edit a pass-1/graph prompt, rerun, and you
  still get the cached outline. Pass `--fresh`. Pass-2 generation itself is not
  cached; each selected lesson spends provider calls again.
- Without `--budget` the model often sets its own budget to 1, and a 1/1 pass
  rate measures nothing.
- Without `--pages`, local Ollama pass 1 still fires one model call per chunk.
  Gemini and DeepSeek instead use bounded batches (max 32 chunks/about 36k
  runes), but a full textbook is still a paid, non-trivial run.
- `--repair` no longer exists. Do not re-add it without a new measured design;
  both local 4B and DeepSeek failed the existing contract. Rejection memory is
  the current replacement and does not add a dedicated API call.
- DeepSeek has no embedding model configured by default, so its chunk ranking is
  source/graph order unless an embedder is explicitly supplied. Local Thai
  ranking must use `bge-m3`, never `nomic-embed-text`.
- The cumulative provider report is per process. In the final normal generation
  process `TOTAL=14`; the separate paid semantic regression test used one
  additional DeepSeek request and is not part of that 14.
- Changing `--embed-model` invalidates every stored vector; the dimension changes
  too (bge-m3 1024, nomic 768).
- **`outline-v2.json` caches are dead.** They were built before topics carried a
  kind, so they still contain apparatus concepts and nothing in the code can
  identify them any more. The cache key is `outline-v3`; the old files are left
  on disk but never read. Extraction caches (`pages-v3.json`) are untouched, so
  the expensive Docling run is not repeated.
- A topic whose kind is missing or unrecognised is kept as content. Silence from
  the model therefore looks exactly like "everything is content" — check the
  `outline/filter` progress line, which reports how many apparatus and furniture
  topics were dropped. Zero on a teacher's edition means the classifier is not
  working, not that the book is clean.
- Re-running a structural check over old `.scratch/*/run-*.json` files is the
  cheapest way to test a new rule — `examgen.CheckWellFormed` is exported for
  exactly that, and it caught a false positive of mine that would otherwise have
  shipped.
