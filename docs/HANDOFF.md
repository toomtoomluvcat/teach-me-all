# Handoff — exam-quality prototype

Last updated 2026-08-02. Supersedes the earlier copy in the OS temp directory;
this file is canonical, delete that one.

Repo: `E:\contribute\teach-me-all`. Branch `prototype/exam-quality`, two commits
ahead of `main`, nothing pushed — the repo has no remote. Work done after those
commits is uncommitted; see "Uncommitted work" below.

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

## Uncommitted work since `9a0d935`

Everything below is in the working tree, builds, and `go vet` is clean. It is not
committed.

- `examgen/wellformed.go` — the nine structural checks and the duplicate gate.
- `llm/stats.go` — per-call timing, labelled via a context value.
- `llm/calctool.go` — the calculator tool loop.
- Gate reordering in `gate.go` (`RunCheapGates` / `AddJudgeGates`).
- Concurrency: `Deps.Parallel`, parallel `outline/map`, the two judges running
  together, `safeProgress()` in `ui.go`.
- `pdfx/poppler.go` — the poppler path and `ExtractAuto`.
- Default embedder changed to `bge-m3`.

## Findings that will not be obvious from the code

- **`nomic-embed-text` cannot read Thai.** Cosine 1.0000 for every pair of Thai
  sentences from the same chapter, whether the same question or not. Gap between
  "duplicate" and "different" is zero. `bge-m3` gives 0.95 vs 0.60, gap 0.31.
  This also means chunk ranking and `--scope` were random on Thai before the
  switch. bge-m3 is 1024-dim, nomic is 768 — changing embedder invalidates every
  stored vector.
- **No single PDF extractor works.** Thai handout → the Go path (poppler drops
  the NIKHAHIT of ำ silently, turning "คำ" into the different, valid word "คา").
  arXiv → poppler (the Go path gets no glyph geometry at all, every run reporting
  X, W, FontSize of zero). Biology → poppler (the Go path lost every space:
  "Theenergystoredincarbohydrate"). Each new document produced a new rule.
  `--extract=auto` now decides per document and prints which it chose.
- **Letter count cannot detect missing spaces** — that was a real bug in `auto`
  and is why `spacingBroken()` exists.
- **Tools and a `format` JSON schema are mutually exclusive in Ollama.** With
  both set the grammar wins, no tool call is emitted, and the model quietly
  answers from its own head. Generation is two turns because of this.
- **Correcting a 4B model does not work; preventing the error does.** The repair
  loop repaired 0 of 4, twice. The calculator tool took arithmetic failures from
  5 to 0 on the same document.
- **Model swapping, not the GPU, is the biggest time sink.** A biology run spent
  4m39s of 11m54s on model loading, because the 4B generator and bge-m3 cannot
  both stay resident once KV cache is counted. Generation itself runs at 35–55
  tok/s, which is healthy and means 6 GB is not the bottleneck.
- **The timing report's `share` column is now wrong.** It divides by the sum of
  per-call durations, which exceeds wall clock once calls run concurrently. Wall
  clock needs measuring separately.

## Storage design, as agreed in conversation

Extraction produces exactly two shapes: `Page{Number, Text}` and
`Chunk{ID, Page, StartOff, EndOff, Text, LessonID}`. Offsets are **rune** offsets
within a page, never bytes and never document-global. Chunks target 1200 runes
with 150 overlap and never cross a page boundary, so a cited page number is
always exactly true. Real sizes: 19 biology pages → 46 chunks; 3 Thai pages → 6.

Tables to create: `documents` (with `sha256` and a **pinned `extract_mode`** —
re-extracting with a different extractor shifts every offset and breaks every
stored citation), `document_pages` (raw page text, so re-chunking never requires
re-running OCR), `chunk_sets` (versioned, so changing chunk size does not orphan
questions that reference old chunks), `chunks` (page_no, ordinal, start_off,
end_off, text, `vector(1024)`, HNSW index).

Three decisions that are painful to reverse:

1. Store `chunks.text` denormalised rather than slicing `document_pages` at read
   time. A stored question must stay verifiable after the source file is deleted.
2. Never re-chunk in place. New chunking run → new `chunk_set`.
3. Offsets are runes. Thai is 3 bytes per character; a byte offset anywhere cuts
   characters in half. Postgres `substring(text FROM n FOR m)` counts characters
   and lines up; `substr()` on `bytea` does not.

## Open question the owner has not answered

**The OCR path emits Markdown, the other two emit plain text.** `typhoon-ocr` is
instructed to render tables as HTML and equations as LaTeX. So one
`document_pages.text` column would hold two formats depending on mode and page
content, and `quote_verbatim` would have to match against `<td>` and `$…$`.
Options put to the owner, not yet chosen: strip markup before storing and keep
the marked-up version in a separate column, or tell typhoon-ocr to emit plain
text and lose table structure. Nothing was implemented either way.

## Environment on this machine

- Ollama 0.32.5 at `%LOCALAPPDATA%\Programs\Ollama`, **not on PATH**. Running
  with `OLLAMA_NUM_PARALLEL=4` and `OLLAMA_MAX_LOADED_MODELS=2` (set as user env
  vars; a restart is needed for changes to take).
- Models: `scb10x/typhoon2.5-qwen3-4b`, `bge-m3`, `nomic-embed-text`,
  `scb10x/typhoon-ocr1.5-3b`.
- poppler 25.07 via winget, **also not on PATH**; `pdfx/tools.go` globs the
  winget Packages directory. Git for Windows ships an Xpdf 4.00 binary also
  called `pdftotext` that *is* on PATH and is worse at Thai — `findTool`
  deliberately looks past it.
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
2. **Fix model swapping** before trying a larger model. 39% of a run went to
   loading. A third model on 6 GB will make it worse.
3. **Then** try a larger model on `judge/source` only — the true-distractor
   defect is the only thing left that a bigger model fixes.
4. Decide the Markdown-vs-plain-text question above.
5. Commit the uncommitted work.

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
  range). Edit a prompt, rerun, and you get the cached outline. Pass `--fresh`.
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
