# Handoff — exam-quality prototype

Last updated 2026-08-03. Supersedes the earlier copy in the OS temp directory;
this file is canonical, delete that one.

Repo: `E:\contribute\teach-me-all`. Branch `prototype/exam-quality`. HEAD is
`4aacc9f`; nothing is pushed and the working tree is clean.

The previous session ran out of budget mid-decision. Everything it decided is
written down here. **Start at items 1 and 2 of "What to do next"** — item 1 is
a live problem, not an improvement: `needs_the_source` currently rejects about
84% of drafts on a mis-calibrated criterion.

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

Seven gates now. Three are model-backed — though only two model calls are
made — and four are deterministic or locally reproducible:

| gate | decided by |
|---|---|
| `well_formed` — ten structural checks, including teacher-guide metadata | Go |
| `quote_verbatim` | Go |
| `arithmetic` | Go |
| `not_a_duplicate` — embedding similarity vs accepted questions | Go + embedder |
| `answerable_blind` — is it clear what is being asked | model |
| `needs_the_source` — could it be answered *without* the passage | model (same call) |
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
step returns `{kind, topics}` per chunk, where kind is `content`, `apparatus`
(answer keys, assessment rubrics, scoring guides, test banks, learning
objectives, lesson plans, teaching hours) or `non_content` (page furniture,
replacing the old `NON_CONTENT` sentinel). `BuildEvidenceGraph` admits only
`content` chunks, so an answer-key page never joins a lesson.

This costs no extra provider call: the map step was already reading every chunk.
A Thai/English phrase list was written first, measured, and then deleted — it
scored 36 of 217 concepts on the cached biology graph with no false positives,
but it only ever knew the wording of the one publisher it had been read from.

Three things were measured, in this order, all on the same 254-page book. Chunks
dropped out of 392:

| what changed | dropped |
|---|---:|
| kind per topic, first prompt | 238 |
| plus the "teacher instructions wrap real content" rule | 184 |
| kind moved to the chunk | 174 |
| plus `SmoothPassageKinds` | **131** |

The first number was a disaster: kidney function, blood pressure, gas exchange
and HIV all classified as apparatus, because a teacher's edition wraps subject
prose in instructions to the teacher and the model read the wrapper. The prompt
now says so explicitly, and ends with "when you cannot decide, choose content".

`SmoothPassageKinds` is the structural half and is not a phrase list either.
Apparatus runs in blocks — an answer key covers whole pages — so a one- or
two-chunk apparatus run sitting between content chunks is the classifier
flickering, not a one-chunk answer key. The run-length histogram on the real book
splits cleanly: 18 runs of one and 7 of two, then real sections of 8, 9, 10, 22
and 34. It only ever rescues, never drops, because a surviving rubric is caught
by the question-level gates while a lost passage is lost silently.

After all four steps, 27 of the 217 concepts from the old cached graph are gone
entirely, and 23 of those are unambiguous apparatus. The four that are not are
`กิจกรรม 15.3` (pages 129-131), `หมู่เลือดและการให้เลือด` (135), `กิจกรรม 16.2`
(173) and `ระบบขับถ่าย` (191) — three-chunk runs the smoother will not touch.
No lesson in the resulting 34 is named after assessment.

`examgen.ChunkTopics.UnmarshalJSON` accepts a bare topic list as well as the
object, and a chunk with a missing or unknown kind is treated as content —
wrongly deleting material costs more than letting one rubric reach the
question-level gates, which still ban teacher-guide stems.

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

## The NotebookLM comparison, first run

Protocol agreed before any data was seen, so the verdict could not be argued
with afterwards. Lesson `กลไกการหายใจและการควบคุม`, 15 questions per set, all
three sets shuffled into one sheet with provenance held in a separate key.
Ours came from the whole book on DeepSeek at commit `b43fba6` (30 drafts, 15
passed the gates, 84 API calls, 2m18s, no top-up outside the lesson).
NotebookLM was given the same 14 pages in one condition and the whole 254-page
book in the other, with an identical prompt that deliberately did **not** carry
our ban on teacher-guide questions.

| label | ours | nlm-pages | nlm-book |
|---|---:|---:|---:|
| good | 6 | 8 | 7 |
| no_reading_needed | 8 | 7 | 7 |
| true_distractor | 1 | 0 | 1 |
| off_topic / teacher_guide / ambiguous | 0 | 0 | 0 |
| **good rate** | **40%** | **53%** | **47%** |

**Verdict: INCONCLUSIVE, and we are not ahead.** The pre-agreed rule was +4/15
to pass; we are -2 and -1. Per that rule: do not ship, do not rebuild, fix the
label we lost most to and re-measure with the same protocol.

The finding that matters is not the gap, it is what all three sets share.
**22 of 45 questions are answerable without reading the passage** — ours 8,
NotebookLM 15. Every gate in the pipeline checks whether a question is
well-formed, grounded, arithmetically sound and non-duplicate. Nothing checks
whether it teaches you anything to answer it. That is the next thing to build,
and it is worth more than any further work on the topic classifier.

Two caveats that must travel with these numbers:

- **The labeller was a model, not the owner.** The agreed design was the
  owner's eyes; a subagent with a cold context did this pass because the main
  session had already seen NotebookLM's output and was contaminated. `Q2` had
  rejected LLM judging for a measured reason, and the `teacher_guide` count of
  0 across all 45 is exactly where that blindness would show. The sheet is
  unchanged and can be relabelled by hand.
- **n=15 detects only large differences.** INCONCLUSIVE means not known.

Sheet and key: `.scratch/labelling/`. Labels: `sheet.labelled.csv` in the
session scratchpad, because Excel held the original open.

## `needs_the_source`: what three runs on one lesson actually showed

Same lesson, same budget, same provider, three consecutive states of the code.
Read this before touching that gate; two of the three numbers are traps.

| | round 1 `b43fba6` | round 2 `bc2b85f` | round 3 `9ead599` |
|---|---:|---:|---:|
| drafts | 30 | 21 | 47 |
| passed | 15 | 15 | **4** |
| pass rate | 50% | 71% | 8.5% |
| filled the budget | yes | yes | **no, hit the ceiling** |
| API calls | 84 | 55 | — |

**Round 2's 71% is not the gate working.** DeepSeek omitted `guess_confidence`
entirely, `""` is not `"high"`, so the gate passed all 15 while deciding
nothing — and printed "the passage is needed" 17 times saying so. The repo
already had the cause written down (*DeepSeek JSON mode is syntax-only*) and the
gate ignored it. What round 2 does show is the **prompt** change working:
requiring each answer to be anchored to a specific the passage supplies cut
wasted drafts from 15 to 6.

**Round 3's 8.5% is real but measured with a thumb on the scale.** The fix for
round 2 added, to the blind judge's prompt, *"Do not be modest; an accurate
'high' here is more useful than a cautious 'medium'."* The judge then answered
`high` on 33 of 37. That is my instruction talking, not calibration.

What is *not* an artefact is how often the blind judge is simply right:

| | correct | wrong |
|---|---:|---:|
| round 2 (no confidence instruction) | 17 | 4 |
| round 3 | 34 | 3 |

**Without the passage, the judge answers correctly 81-92% of the time. Random
is 25%.** That is independent of any confidence wording and it agrees with the
blind labeller, who marked 22 of 45 questions answerable unread.

### The design error underneath

The gate uses an LLM as a stand-in for *a Thai upper-secondary student who has
not read this passage yet*. It is not one — it has read the whole subject. So
"the model answered it unread" and "a learner learns nothing from reading" are
not the same claim, and treating them as the same fails nearly every question a
textbook can support. That is why round 3 could not even fill a 15-question
budget from a 19-chunk lesson.

### Decided with the owner, not yet done

1. **Remove the "do not be modest" line and re-measure.** The current
   distribution cannot be trusted for setting any threshold because I biased it.
   One run, ~55-90 calls.
2. **Then replace the proxy (option D).** The criterion should be whether the
   answer depends on a *specific the passage supplies* — a number, a named
   structure, a stated order or condition — not whether a well-read model can
   answer it. The owner's words: *เพราะเนื้อหา ม.ปลายอยู่ในหัวโมเดลอยู่แล้ว.*
   Design is open. A deterministic shape is plausible (does the correct choice
   turn on a token that appears in the chunk and is not general vocabulary?) and
   would cost no model call, but nothing has been tried yet.

Until 1 and 2 land, **do not ship `needs_the_source` as a hard gate** and do not
read round 3's 8.5% as a quality regression — it is a mis-calibrated ruler.

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

1. **Start here: de-bias the blind judge, then replace its job.** Remove the
   "do not be modest" sentence from `blindSystem` in `examgen/prompt.go`, rerun
   the same lesson once, and read the confidence distribution before setting any
   threshold. Then design option D — a criterion based on whether the answer
   turns on a specific the passage supplies, rather than on whether a well-read
   model can answer it. Full reasoning and the three measured runs are in
   "`needs_the_source`: what three runs on one lesson actually showed". Both
   steps were agreed with the owner and neither is started.
   `needs_the_source` is currently a **hard gate that rejects ~84% of drafts** —
   whoever picks this up either finishes the two steps or turns the gate back to
   advisory. Do not leave it as it is.
2. **Round 2 of the NotebookLM comparison is set up and cheap.** Both NotebookLM
   sets are fixed artefacts in `tools/nlm-pages.txt` and `tools/nlm-book.txt` and
   are reused as-is — no browser, no second visit. It costs one generation run
   plus a **fresh** labelling subagent (the previous one has seen the first
   sheet and cannot be reused). Same lesson, same seed `20260803`, same +4/15
   rule. Do not run it until item 1 lands: a run under the current gate cannot
   even produce 15 questions.
3. **Relabel the same 45-question sheet by hand.** The first pass was done by a
   model, which is the thing that was explicitly rejected when the protocol was
   agreed. The sheet is untouched and the key is separate, so this costs only
   the owner's half hour and it either confirms the result or exposes where the
   model judge is blind.
4. Replay the six historical true-distractor questions through
   `judge/source`, once with local 4B and once with DeepSeek. The current paid
   regression proves one Thai paraphrase case, not the whole recurring defect.
5. ~~Filter pedagogy at graph compilation~~ — done and measured on DeepSeek over
   four live runs; see the table above. Two things are still open. **The
   classifier has never been run on the local 4B model**, which may be much worse
   at it and would degrade silently rather than fail — a model that answers
   `content` for everything looks exactly like a clean book, so read the
   `outline/filter` line. And the reduce step returned 34 lessons this time
   against 17 before; more concepts survive, so lessons got thinner. Whether that
   is better or worse for a learner is unmeasured.
6. For known prose subjects, measure `--calc-tool=false`. It should remove the
   three calculator-tool calls seen in the 14-call biology run; keep the
   arithmetic gate regardless. Do not make this the global default without a
   routing rule because quantitative science/math passages need the tool.
7. Obtain a genuine Thai camera/flatbed scan. The official สสวท. benchmark
   is digital (Biology M.5 book 4); pages 60-62 prove Thai text, table, page
   breaks and figure crops, but do not prove robustness to skew/shadows/blur.
   If image-dependent questions are in MVP scope, add `question_assets`;
   otherwise forbid those questions explicitly.
8. **If a clean performance claim matters, do one cold-cache rerun** after
   unloading both models. The instrumentation now shows the batching path itself
   is only 9 embed calls / 3.007s on this workload; the remaining load cost is
   generator/judge model swapping.
9. Only after the quality comparison, decide how much of `examgen/`, `pdfx/`
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
- A chunk whose kind is missing or unrecognised is kept as content. Silence from
  the model therefore looks exactly like "everything is content" — check the
  `outline/filter` progress line, which reports how many apparatus and furniture
  topics were dropped. Zero on a teacher's edition means the classifier is not
  working, not that the book is clean.
- `maxFlickerRun = 2` in `graph.go` was set from a run-length histogram, not
  taste. Raising it to 3 recovers 12 more chunks and starts merging real
  three-chunk answer-key runs back into lessons. Re-derive the histogram before
  changing it; the cached `outline-v3.json` has enough in it to do that offline.
- Re-running a structural check over old `.scratch/*/run-*.json` files is the
  cheapest way to test a new rule — `examgen.CheckWellFormed` is exported for
  exactly that, and it caught a false positive of mine that would otherwise have
  shipped.
