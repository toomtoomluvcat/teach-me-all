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

chunks ──pass 1 (map-reduce)──> course outline (lessons)
                                       │
                        you pick a lesson in the TUI
                                       │
chunks of that lesson ──pass 2──> MCQ (JSON-schema constrained)
                                       │
                                   4 gates
                                       │
              failures ──top-up from other chunks / sibling lessons──┘
                                       │
                          every question kept, pass or fail
```

## The four gates

| # | Checks | Judged by |
|---|--------|-----------|
| 1 | `source_quote` is a verbatim substring of the cited chunk | **Go** — no model involved |
| 2 | Question is answerable with the source hidden | model, second pass |
| 3 | Exactly one choice is defensible | model, second pass |
| 4 | `calculation.expression` evaluates to the answer the model marked correct | **Go** — no model involved |

Gate 4 is a backstop, not the primary defence. Before generating, the model
calls a `calc` tool that Go answers, so it never computes anything itself
(`llm/calctool.go`). The gate then re-checks, because an expression in the
database can be re-verified years later and a tool call that already happened
cannot. Tools and a `format` schema are mutually exclusive in Ollama, so
generation runs as two turns — see VERDICT.md.

Gates 1 and 4 are the trustworthy ones — they are deterministic and re-runnable
forever. Gates 2 and 3 are LLM-as-judge and should be treated as advisory,
especially on a 4B model. Eyeball the first 20 failures before believing them.

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
- DeepSeek is supported through its OpenAI-compatible chat API. Set
  `DEEPSEEK_API_KEY` before running; the default model is `deepseek-chat`.
  DeepSeek has no embeddings endpoint here, so duplicate/ranking embeddings are
  disabled for this provider.
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
go run . --pdf ../../samples/algebra-en.pdf
```

Gemini example (PowerShell):

```powershell
go run . --provider gemini --pdf ../../samples/algebra-en.pdf
```

DeepSeek example:

```powershell
.\protoexam.exe --provider deepseek --pdf ..\..\samples\algebra-en.pdf
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--pdf` | *(required)* | source PDF |
| `--provider` | `ollama` | `ollama`, `gemini`, or `deepseek` |
| `--extract` | `auto` | `auto` and `docling` both run the single supported Docling pipeline; there is no lower-quality fallback |
| `--extract-dir` | `.scratch/<hash>/extract` | output directory for the reusable extraction bundle |
| `--docling-python` | auto-detected | Python executable containing Docling; overrides `DOCLING_PYTHON` |
| `--docling-ocr-engine` | `auto` | auto selects EasyOCR because the default language set includes Thai; pass `rapidocr` explicitly for a Latin-only speed test |
| `--docling-ocr-lang` | `th,en` | comma-separated OCR languages |
| `--docling-ocr-full-page` | `false` | OCR the complete page; normally leave off so native PDF text stays native |
| `--calc-tool` | `true` | model calls a calculator before writing calculation questions. Took arithmetic failures from 5 to 0 — see VERDICT.md |
| `--repair` | `false` | hand rejected questions back with the discrepancy. Measured worthless on 4B |
| `--model` | provider default | generation + judge model; Ollama defaults to `scb10x/typhoon2.5-qwen3-4b`, Gemini to `gemini-2.5-flash`, DeepSeek to `deepseek-chat` |
| `--embed-model` | provider default | Ollama `bge-m3`, Gemini `gemini-embedding-001`, DeepSeek disabled; pass an explicit empty value to disable ranking |
| `--gemini-host` | `https://generativelanguage.googleapis.com` | Gemini API host |
| `--gemini-api-key` | `GEMINI_API_KEY` | Gemini API key; the flag is useful for one-off runs |
| `--gemini-min-interval` | `13s` | minimum delay between Gemini requests; conservative for a 5-RPM free-tier project, pass `0s` to disable |
| `--deepseek-host` | `https://api.deepseek.com` | DeepSeek API host |
| `--deepseek-api-key` | `DEEPSEEK_API_KEY` | DeepSeek API key; the flag is useful for one-off runs |
| `--force-calc` | `false` | only generate calculation questions |
| `--pages` | all | page range, e.g. `10-40` |
| `--fresh` | `false` | ignore the cache and redo extraction/embedding |
| `--extract-only` | `false` | stop after extraction and dump the full text — needs no Ollama at all |
| `--scope` | *(none)* | free-text focus; chunks are ranked against this instead of the lesson title |
| `--budget` | *(model decides)* | override how many questions the lesson should have |

**Run `--extract-only` first.** It makes no LLM API calls and tells you whether
the rest of the pipeline has a chance:

```
go run . --pdf ../../samples/thai-book.pdf --extract-only
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

For Gemini and DeepSeek, pass 1 maps all chunks in one structured request, then
uses one reduce request. This changes `outline/map` from one call per chunk to
one call for the whole document; if a provider omits a chunk, only that chunk
is retried individually. The rest of the pipeline still reports its own calls.
The default 13-second spacing is only applied to Gemini. Use `--fresh` when you
want to regenerate a cached outline after changing provider or prompts.

At the end of every process, including one that exits after an API error, the run
prints a cumulative call report. With Gemini or DeepSeek, `TOTAL` is the exact
number of provider HTTP requests attempted by the process, including retries and
429/5xx responses. The `embed` row counts batch embedding requests, not
individual texts; the other rows identify the pipeline stage (`outline/map`,
`outline/reduce`, `generate`, `judge/*`, `repair`, and `calc-tool`).

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
main.go        TUI shell            — throwaway
ui.go          frame rendering      — throwaway
examgen/       pure logic           — LIFTABLE into production
llm/           Ollama client        — mostly liftable
pdfx/          extraction           — liftable, but see the risk note below
```

`examgen/` must not import `llm/` concretely or print anything. It takes interfaces
and returns values. That is what makes it liftable.

## What extraction actually does

`auto` is **Docling standard pipeline + pypdfium2**. It recovers reading order,
tables, native text, OCR text, and figure crops in one local pass. EasyOCR
`th,en` is the default so Thai remains supported for bitmap text and scans.
The same result supplies Markdown for reading and plain text for exam chunks.
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
- The arithmetic evaluator in `examgen/calc.go` is hand-written rather than
  pulled from an expression library. A grammar that cannot express a function
  call or a variable cannot be talked into doing anything but arithmetic, and
  these expressions come from a language model and get stored and re-run for
  years.
- Field-level API details for all of this are in
  `docs/research/ollama-and-go-libs-api-spec.md`.
