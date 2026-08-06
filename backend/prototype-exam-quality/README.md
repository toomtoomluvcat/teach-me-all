# PROTOTYPE — exam quality

**Throwaway code. Do not import this from production. Do not ship the TUI.**

## The question this answers

> Does a chunk-grounded, gate-verified generation pipeline produce multiple-choice
> questions that a human can actually interpret and answer — and does it beat
> NotebookLM on the same source file?

That is the *only* question. Not schema, not API design, not UI. The project owner's
stated fear is "ข้อสอบไม่เสถียร ตีความไม่ได้" — questions that are vague or
uninterpretable. Everything here exists to measure that and nothing else.

## What it does

```
PDF ──extract──> text ──chunk──> chunks ──embed──> vectors
                   │
                   └─ prints a sample so you can SEE if extraction is garbage

chunks ──pass 1 (map-reduce)──> evidence graph ──> course outline (lessons)
                                       │
                        you pick a lesson in the TUI
                                       │
      graph atoms + bounded 2-hop context ──set pass──> MCQ set (JSON-schema constrained)
                                       │
                              deterministic QC gates
                                       │
       failures ──bounded rejection memory ──next generate / top-up─┘
                                       │
                          every question kept, pass or fail
```

## The five QC gates

| # | Checks | Judged by |
|---|--------|-----------|
| 1 | Structural shape, exactly one correct choice, and no teacher-guide metadata | **Go** — no model involved |
| 2 | Cited chunk is not an explicit pre-learning check / answer-key section | **Go** — no model involved |
| 3 | `source_quote` is a verbatim substring of the cited chunk | **Go** — no model involved |
| 4 | `calculation.expression` evaluates to the answer the model marked correct | **Go** — no model involved |
| 5 | Question is not a duplicate of an accepted question | **Go + embedder** — no judge involved |

The QC gates are deliberately a backstop, not a complete educational-quality
grader. Before generating, the model
calls a `calc` tool that Go answers, so it never computes anything itself
(`llm/generation/calctool.go`). The gate then re-checks, because an expression in the
database can be re-verified years later and a tool call that already happened
cannot. Tools and a `format` schema are mutually exclusive in Ollama, so
generation runs as two turns — see VERDICT.md.

The source-dependency and per-choice semantic judges remain available for
advisory evaluation, but they are not hard gates in the core path. This keeps
the acceptance rule focused on catching malformed, fabricated, duplicated, or
arithmetically wrong generations rather than asking the generator to grade its
own educational quality.

Pass 1 gives every concept a stable graph ID with page/chunk provenance and
evidenced `co_occurs` / `follows` edges. Generation sees only concepts supported
by its current chunk. Failed drafts are not repaired. The newest four failures
are kept as compact negative memory (stem, choices, gate reason, and semantic
choice verdicts) and attached to the next normal generation call. This adds no
provider request: top-up was already needed to fill the budget. The model is
told to ask a materially different question rather than paraphrase the rejected
one. The generation contract also excludes learning objectives, assessment
guidance, and classroom activities; a deterministic pre-judge gate catches
known Thai and English teacher-guide phrases if the model ignores that rule.

Pass 1 always ends with a compile step that splits the
graph into atomic claims (`A###`) with exact chunk provenance, supported
question forms, and source-stated conditions. The selected lesson receives its
own chunks plus bounded two-hop context from adjacent concepts. A
deterministic coverage contract (`S##`) then gives the writer one distinct
atom/operation per target. Questions must return the slot, atom, and cited
chunk IDs, so a verbatim quote cannot silently drift away from the evidence it
is meant to support.

Anthropic cannot enable citations and structured outputs at the same time
(400 error), so `source_quote` + server-side substring check is what production
will have to do anyway. Building it here is not throwaway work.

## Requirements

- Default provider: [Ollama](https://ollama.com) running on `http://localhost:11434`
- Ollama models:
  ```
  ollama pull scb10x/typhoon2.5-qwen3-4b
  ollama pull bge-m3
  ```
- Gemini is also supported through the Gemini REST API. Set `GEMINI_API_KEY`
  before running; the default Gemini models are `gemini-2.5-flash` and
  `gemini-embedding-001`.
- `--provider openai` talks to any host that speaks the OpenAI
  `/chat/completions` wire format — DeepSeek, OpenRouter, Groq, Together,
  Mistral, Fireworks, and, for a local model, vLLM, llama.cpp, LM Studio, or
  Ollama's own `/v1` endpoint. Give it `--base-url` and `--model`; the key comes
  from `LLM_API_KEY` or `--api-key`, and a local server usually needs none.
  Whether embeddings work is a property of the host, not the protocol, so
  `--embed-model` stays empty unless you name one.
- Preset provider names fill in a base URL, key variable, and default model, and
  everything they set can still be overridden on the command line. There is one
  today: `--provider deepseek` (`DEEPSEEK_API_KEY`, `deepseek-chat`). Adding a
  vendor needs a base URL, not code.
- For a local model, `--provider ollama` and `--provider openai --base-url
  http://localhost:11434/v1` are not equivalent. The native Ollama path keeps
  the measured one-call-per-chunk map step and defaults `--embed-model` to
  `bge-m3`; the OpenAI-compatible path maps chunks in large batches and leaves
  embeddings off unless you name a model. Batched mapping suits a
  large-context server (vLLM, llama.cpp with a big window); for a small local
  model served by Ollama, prefer `--provider ollama`.
- The CLI also loads the nearest `.env` from the working directory or its
  parents. Already-exported environment variables take precedence.
- For the best `--extract=auto` path: Python 3.12 plus the project-local
  Docling runtime. Set it up once from PowerShell:
  ```powershell
  .\setup-docling.ps1
  ```
  The runtime lives at the repo-level `.scratch/docling-venv`; it is reused by
  `go run` and `protoexam.exe` and never calls Gemini, DeepSeek, or another
  generative API. Models download only on the first run. Docling is required;
  extraction fails clearly when this runtime is unavailable.

Target machine is 6 GB VRAM, so models are loaded and unloaded one at a time.

## Run

```
cd backend/prototype-exam-quality
go run ./cmd/protoexam --pdf ../../samples/algebra-en.pdf
```

Gemini example (PowerShell):

```powershell
go run ./cmd/protoexam --provider gemini --pdf ../../samples/algebra-en.pdf
```

DeepSeek example:

```powershell
.\protoexam.exe --provider deepseek --pdf ..\..\samples\algebra-en.pdf
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--pdf` | *(required)* | source PDF |
| `--provider` | `ollama` | `ollama`, `openai`, `gemini`, or the `deepseek` preset |
| `--extract` | `auto` | `auto` and `docling` both run the single supported Docling pipeline; there is no lower-quality fallback |
| `--extract-dir` | `.scratch/<hash>/extract` | output directory for the reusable extraction bundle |
| `--docling-python` | auto-detected | Python executable containing Docling; overrides `DOCLING_PYTHON` |
| `--docling-ocr-engine` | `auto` | auto selects EasyOCR because the default language set includes Thai; pass `rapidocr` explicitly for a Latin-only speed test |
| `--docling-ocr-lang` | `th,en` | comma-separated OCR languages |
| `--docling-ocr` | `auto` | `auto` uses the PDF's native text layer for digital PDFs and enables OCR for scanned PDFs; `on`/`off` override it |
| `--docling-formulas` | `auto` | conservative by default; auto warns when formula-like prose is detected, while `on` runs Docling's heavier formula-to-LaTeX pass |
| `--docling-ocr-full-page` | `false` | OCR the complete page; normally leave off so native PDF text stays native |
| `--calc-tool` | `true` | model calls a calculator before writing calculation questions. Took arithmetic failures from 5 to 0 — see VERDICT.md |
| `--model` | provider default | generation + review model; Ollama defaults to `scb10x/typhoon2.5-qwen3-4b`, Gemini to `gemini-2.5-flash`, the `deepseek` preset to `deepseek-chat`. `--provider openai` has no default and requires this |
| `--embed-model` | provider default | Ollama `bge-m3`, Gemini `gemini-embedding-001`, `openai` off unless named; pass an explicit empty value to disable ranking |
| `--gemini-host` | `https://generativelanguage.googleapis.com` | Gemini API host |
| `--gemini-api-key` | `GEMINI_API_KEY` | Gemini API key; the flag is useful for one-off runs |
| `--gemini-min-interval` | `13s` | minimum delay between Gemini requests; conservative for a 5-RPM free-tier project, pass `0s` to disable |
| `--base-url` | preset default | OpenAI-compatible base URL, without `/chat/completions` |
| `--api-key` | `LLM_API_KEY` | API key for the OpenAI-compatible provider; the flag is useful for one-off runs |
| `--force-calc` | `false` | require arithmetic on every question; keep `skill` as cognitive demand |
| `--pages` | all | page range, e.g. `10-40` |
| `--fresh` | `false` | ignore the cache and redo extraction/embedding |
| `--extract-only` | `false` | stop after extraction and dump the full text — needs no Ollama at all |
| `--scope` | *(none)* | free-text focus; chunks are ranked against this instead of the lesson title |
| `--budget` | *(model decides)* | override how many questions the lesson should have |
| `--set-candidates` | `3` | generate independent set candidates and keep the highest deterministic QC/diversity score |
| `--contract-preflight` | `true` | repair/drop deterministic slot defects before generation; no model call |
| `--stop-on-full-set` | `false` | stop generating candidates once one covers every contract slot; saves calls, costs set variety |

**Run `--extract-only` first.** It makes no LLM API calls and tells you whether
the rest of the pipeline has a chance:

```
go run ./cmd/protoexam --pdf ../../samples/thai-book.pdf --extract-only
```

The built executable can be reused without rebuilding:

```powershell
.\protoexam.exe --extract-only --extract auto --pdf ..\..\samples\openstax-college-algebra.pdf
```

It writes a reusable bundle under `.scratch/<hash>/extract/` (or
`--extract-dir`): `manifest.json`, combined and per-page Markdown/plain text,
Docling's structural JSON, and figure-level image crops referenced from the
Markdown. It does **not** render and store every page as an image. That keeps
the reading bundle small while preserving diagrams and photos that would be
lost by text extraction. The older `extracted.txt` is also written for
compatibility. Physical pages are never renumbered or dropped: a page with no
serialized content is retained as an empty per-page file and an empty section
in `document.md`.

Extraction reports the active stage and elapsed time while Docling is running,
then the exact page/figure totals and bundle-write progress. `--extract-only`
does not wait for keyboard input; a normal generation run shows `[enter]` to
continue and `[q + enter]` to quit.

Extraction and embedding are cached under `.scratch/` (gitignored) because they are
slow and rerunning them while iterating on prompts is a waste.

For Gemini and OpenAI-compatible providers, pass 1 maps chunks in bounded structured batches of at
most 32 chunks and about 36,000 runes, then uses one reduce request. The CLI
prints this base call count before sending anything. Batches run concurrently
up to `--parallel`; Gemini still spaces request starts by 13 seconds. If JSON is
truncated, only that batch is split and retried; if a provider omits a chunk,
only that chunk is retried. Terminal network/auth errors cancel pending batches.
Use `--fresh` to regenerate a cached outline after changing provider or prompts.

At the end of every process, including one that exits after an API error, the run
prints a cumulative call report. With a hosted provider, `TOTAL` is the exact
number of provider HTTP requests attempted by the process, including retries and
429/5xx responses. The `embed` row counts batch embedding requests, not
individual texts; the other rows identify the pipeline stage (`outline/map`,
`outline/reduce`, `generate`, `judge/*`, and `calc-tool`). Rejection memory is
part of the next `generate` prompt, not a separate API call.

## Finish line

The prototype is done when all of these exist:

1. Gate pass rate recorded for an English PDF and a Thai PDF.
2. You have read 20 gate-passing questions and marked each acceptable / not.
3. The same PDF has been through NotebookLM and the two sets compared side by side.

Then: write the verdict into `VERDICT.md` here, lift the validated pieces of
`examgen/` into the real backend, and push this whole directory to a throwaway
branch. Main keeps the decision, not the prototype.

## Layout

```
cmd/protoexam/   process entrypoint — throwaway
app/             CLI orchestration, TUI, cache/artifacts, benchmark — throwaway
examgen/         model, evidence, gates, generation              — LIFTABLE
llm/             core, providers, generation, judging            — mostly liftable
pdfx/            extract, bundle                                  — liftable, see risk note
```

`examgen/` must not import `llm/` concretely or print anything. It takes interfaces
and returns values. That is what makes it liftable.

## What extraction actually does

`auto` is **Docling standard pipeline + pypdfium2**. It first inspects the PDF's
embedded text layer. Digital PDFs keep native PDFium text and do not pay the OCR
cost; scanned or mixed PDFs enable OCR. The same result supplies Markdown for
reading and native-layer text for exam chunks when it is reliable. Formula
recognition is deliberately opt-in with `--docling-formulas on` because its VLM
model is substantially heavier than the normal extraction path.
There is deliberately no text-only or LLM OCR fallback: a Docling failure is an
extraction failure, not permission to silently return a lower-quality document.

## Known risks going in

1. **Thai camera/flatbed scans.** The official ม.5 benchmark is a digital PDF.
   Pages 60-62 prove Thai text, tables, page boundaries, OCR labels, and figure
   crops, but not skew, shadows, blur, or paper texture from a genuine scan.
2. **A 4B model judging gates 2 and 3.** If it rejects good questions we will
   misread the pipeline as broken. Cross-check by hand on the first run.
3. **4B Thai output quality** is a separate failure mode from (1). Do not conflate
   them — read the extracted text before blaming the model.
4. **The judge is the same model as the generator.** A model grades its own work
   softly. Two models do not fit in 6 GB and swapping per question would make a
   run take hours. This is why gates 1 and 4 are deterministic.

## Notes for production, not for here

- `pdftoppm` is fine for a prototype but is a GPL external binary. For the real
  server, `github.com/klippa-app/go-pdfium` in `webassembly` mode rasterises
  with no cgo and no external binary, under MIT + Apache-2.0. Avoid `go-fitz`:
  it is AGPL and §13 reaches network users.
- The arithmetic evaluator in `examgen/model/calc.go` is hand-written rather than
  pulled from an expression library. A grammar that cannot express a function
  call or a variable cannot be talked into doing anything but arithmetic, and
  these expressions come from a language model and get stored and re-run for
  years.
- Field-level API details for all of this are in
  `docs/research/ollama-and-go-libs-api-spec.md`.
