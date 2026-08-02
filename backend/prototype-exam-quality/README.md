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
  ollama pull scb10x/typhoon-ocr1.5-3b     # only for --extract=ocr
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
- For `--extract=ocr`: `pdftoppm` (poppler) on PATH to rasterize pages.

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
| `--extract` | `auto` | `auto` picks per document, `text` = ledongthuc/pdf, `poppler` = pdftotext, `ocr` = rasterise + typhoon-ocr |
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

**Run `--extract-only` first, on both PDFs, before installing anything.** It is
free and instant and it tells you whether the rest of the pipeline has a chance:

```
go run . --pdf ../../samples/thai-book.pdf --extract-only
```

It writes the whole extracted text to `.scratch/<hash>/extracted.txt`. Read it.

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

Two things had to be fixed before the text layer was usable, both in `pdfx`:

1. **`GetPlainText` is not usable.** It emits every PDF text run on its own
   line, so "DLD-01" arrives as three lines and a Thai word split across runs
   arrives split. `GetTextByRow` groups runs by Y position instead; `pageText`
   then joins them left to right, inserting a space only where the gap is wide
   enough to be a real one. Thai gets a wider threshold because it does not put
   spaces between words at all.
2. **SARA AM (ำ) loses its NIKHAHIT.** It is drawn as two glyphs and many PDFs
   have no character map for the one on top, so "คำ" extracts as "ค" + space +
   "า". Without repairing this, gate 1 fails on every Thai question — the model
   writes "คำ" and the source says "ค า". `repairThai` puts it back.

Lines that are more than 30% U+FFFD are dropped: diagrams embed fonts with no
character map and what comes out is noise that costs tokens and confuses pass 1.

## Known risks going in

1. ~~**Thai PDF extraction.**~~ Checked against a real Thai course handout:
   `ledongthuc/pdf` reads Thai correctly once the two fixes above are in place.
   Still verify with `--extract-only` on each new document — a scanned PDF has
   no text layer at all and needs `--extract=ocr`.
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
