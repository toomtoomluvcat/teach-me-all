# AI Exam Generation — Primary-Source Research

**Repo:** `teach_me_all` (Go backend at `E:\contribute\teach-me-all\backend`)
**Researched:** 2026-08-01
**Scope:** LLM provider choice, structured output, tool-use for arithmetic, PDF ingestion, RAG in Postgres, question-quality techniques, async jobs, and schema gaps.

> **Sourcing rule applied:** every external claim below links to an official doc, an official SDK repo/reference, a spec, or an arXiv paper. Version numbers for Go modules were read from the **Go module proxy** (`https://proxy.golang.org/<module>/@latest`), which is authoritative for "what does `go get` resolve to today". Section 8 is my own reading of this repo's code, not external research.

---

## Summary / recommendations

1. **Use Anthropic `github.com/anthropics/anthropic-sdk-go` v1.61.0 as the primary provider.** It is post-1.0 with a documented SemVer policy, requires Go 1.24+ (you are on 1.25), and natively supports PDF input, JSON-schema-constrained output, and tool use. *Confidence: high.*
2. **Do not send raw PDFs to the model for the course-splitting pass.** Extract text in Go first, then send text. Native PDF is 600 pages/32 MB max on Anthropic and costs ~7,000 tokens per 3 pages with vision — roughly 2,300 tok/page, which is untenable for a 400-page textbook. *Confidence: high.*
3. **Use `github.com/ledongthuc/pdf` (BSD-3, Go-native) for text-layer extraction, and `pdfcpu` (Apache-2.0) only for structural work** (page counts, splitting, validation). `pdfcpu` does **not** do plain-text extraction. `unipdf` is the best extractor but is **commercial-licence-only** — avoid unless you buy a licence. *Confidence: high.*
4. **Scanned/image-only PDFs will not work with any pure-Go extractor.** Detect them (text layer yields ~0 chars) and route those *specific pages* to the model as images/native PDF, or reject the upload with a clear error. *Confidence: high.*
5. **Force MCQ shape with Anthropic structured outputs (`output_config.format` → `json_schema`), which is guaranteed valid via constrained decoding** — not best-effort. Budget for a first-request grammar-compile latency; grammars cache for 24h. *Confidence: high.*
6. **Critical constraint: Anthropic citations and structured outputs are mutually exclusive** (400 error if both are set). If you want a provenance span back to the source chunk, put the span in your *own* schema field (`source_quote`) and verify it server-side by exact substring match against the chunk — do not rely on the citations API. *Confidence: high.*
7. **For the math requirement, use a self-hosted calculator tool, not the provider's code-execution sandbox.** Anthropic's sandbox has no internet, runs per-request containers you must thread by ID, and is billed hourly; a deterministic Go tool is cheaper, auditable, and reproducible at persist-time. *Confidence: medium-high.*
8. **`--force` calculation-only mode is a prompt + schema + validation triple**, not just a prompt: add `question_type: "calculation"` as a schema `enum`, require `computation` fields, and reject any generated item whose answer was not produced by a tool call. *Confidence: medium.*
9. **For retrieval, start with Postgres full-text search (`tsvector` + `websearch_to_tsquery` + `ts_rank_cd`) — zero new infrastructure.** Add `pgvector` 0.8.6 with an HNSW index only when FTS demonstrably misses. Hybrid (RRF) is documented by pgvector as the recommended combination. *Confidence: medium-high.*
10. **If you do add embeddings, `text-embedding-3-small` at $0.02/1M tokens is the cheapest credible option**; a 400-page book is roughly 200–300K tokens, i.e. under one US cent to embed. Cost is not the deciding factor — operational complexity is. *Confidence: high.*
11. **Use `riverqueue/river` v0.42.0 for async ingest jobs.** It is Postgres-backed (matches your existing stack, no Redis), actively released, and supports transactional enqueue. Note its licence is **MPL-2.0**, not MIT. *Confidence: medium-high.*
12. **The current schema cannot express this product at all** — `VARCHAR(100)` on question/choice content, no `is_correct` on `choices`, no documents/chunks tables, no attempt history, no ordering, and a `models.Question.IsCorrect` field that does not exist in any migration and therefore silently reads `false` forever. See §8. *Confidence: high (read directly from the code).*

---

## 1. LLM provider + Go SDK

### 1.1 Version and maturity (verified via the Go module proxy, 2026-08-01)

| SDK | Module path | Latest version | Published | Go requirement |
|---|---|---|---|---|
| Anthropic | `github.com/anthropics/anthropic-sdk-go` | **v1.61.0** | 2026-07-24 | Go 1.24+ |
| Google Gemini | `google.golang.org/genai` | **v1.66.0** | 2026-07-29 | see note |
| OpenAI | `github.com/openai/openai-go/v3` | **v3.49.0** | 2026-07-31 | Go 1.25+ (v3.45.0+) |

Sources: [proxy.golang.org/github.com/anthropics/anthropic-sdk-go/@latest](https://proxy.golang.org/github.com/anthropics/anthropic-sdk-go/@latest), [proxy.golang.org/google.golang.org/genai/@latest](https://proxy.golang.org/google.golang.org/genai/@latest), [proxy.golang.org/github.com/openai/openai-go/v3/@latest](https://proxy.golang.org/github.com/openai/openai-go/v3/@latest).

**Maturity statements, quoted from the maintainers:**

- **Anthropic** — the [README](https://github.com/anthropics/anthropic-sdk-go/blob/main/README.md) carries **no** alpha/beta warning (earlier releases of this SDK did). It states only *"Go 1.24+"* under requirements, and points to [platform.claude.com/docs/en/api/sdks/go](https://platform.claude.com/docs/en/api/sdks/go). Being at v1.61.0 under SemVer, the major version itself is the stability signal. *I could not locate an explicit "Semantic versioning" section in the README* — see Open Questions.
- **Google** — [pkg.go.dev/google.golang.org/genai](https://pkg.go.dev/google.golang.org/genai) lists the module as **v1.x, Apache-2.0 / BSD-3-Clause**. The [README](https://github.com/googleapis/go-genai/blob/main/README.md) contains no GA/stability prose but does warn of **upcoming breaking changes to `GenerateVideos` in SDK v2.0.0** — i.e. a v2 is planned.
- **OpenAI** — the [README](https://github.com/openai/openai-go/blob/main/README.md) opens with *"The latest version of this package has small and limited breaking changes."* Its "Semantic versioning" section says the package *"generally follows SemVer conventions, though certain backwards-incompatible changes may be released as minor versions"*. It also states: *"SDK v3.45.0 and later require Go 1.25 or later. If your application must remain on Go 1.22–1.24, pin SDK v3.44.0, the final compatible release."* This is the least conservative stability posture of the three.

### 1.2 Capability matrix

| Capability | Anthropic | Gemini | OpenAI |
|---|---|---|---|
| **(a) Native PDF/document input** | Yes — `document` content block, `base64` / `url` / Files API `file_id`. Go: `anthropic.NewDocumentBlock(anthropic.URLPDFSourceParam{...})` or `Base64PDFSourceParam`. [PDF support](https://platform.claude.com/docs/en/build-with-claude/pdf-support) | Yes — `Part.InlineData` (`*genai.Blob`, mime `application/pdf`) or `Files.Upload`/`UploadFromPath`. [Document processing](https://ai.google.dev/gemini-api/docs/document-processing) | Yes — file uploads via `io.Reader` with filename/content-type. [README](https://github.com/openai/openai-go/blob/main/README.md) |
| **(b) Structured / schema-constrained output** | Yes — `OutputConfig: anthropic.OutputConfigParam{Format: anthropic.JSONOutputFormatParam{Schema: ...}}`. **Guaranteed valid** via constrained decoding. [Structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs) | Yes — `GenerateContentConfig.ResponseMIMEType` + `.ResponseSchema`. [pkg.go.dev](https://pkg.go.dev/google.golang.org/genai) | Yes — `json_schema` + `strict: true`, with schema generation and type-safe extraction shown in the README |
| **(c) Tool use** | Yes — `ToolParam` + `ToolUnionParam`, `StopReason == "tool_use"`, plus a beta `BetaToolRunner` that drives the loop. [Tool use overview](https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview) | Yes — `Tool`, `FunctionDeclaration`, `FunctionCall`, `FunctionResponse`, `FunctionCallingConfig` | Yes — function definition, invocation handling, result integration documented in README |
| **Embeddings** | **No first-party embeddings endpoint.** Anthropic partners with Voyage AI. | Yes — `Models.EmbedContent`, `Batches.CreateEmbeddings` | Yes — `text-embedding-3-*` |
| **Server-side code execution** | Yes — `code_execution_20260521` (GA, no beta header). [Code execution tool](https://platform.claude.com/docs/en/agents-and-tools/tool-use/code-execution-tool) | Yes — `ToolCodeExecution`. [Code execution](https://ai.google.dev/gemini-api/docs/code-execution) | Not researched |

### 1.3 Anthropic model IDs and pricing

Read from the bundled `claude-api` skill reference (cached 2026-06-24), **not from memory**:

| Model | Model ID | Context | Input $/1M | Output $/1M |
|---|---|---|---|---|
| Claude Opus 5 | `claude-opus-5` | 1M | $5.00 | $25.00 |
| Claude Sonnet 5 | `claude-sonnet-5` | 1M | $3.00 ($2.00 intro through 2026-08-31) | $15.00 ($10.00 intro) |
| Claude Haiku 4.5 | `claude-haiku-4-5` | 200K | $1.00 | $5.00 |
| Claude Opus 4.8 | `claude-opus-4-8` | 1M | $5.00 | $25.00 |
| Claude Fable 5 | `claude-fable-5` | 1M | $10.00 | $50.00 |

Model IDs are **complete as written — never append a date suffix**. Live lookup: `client.Models.Retrieve(id)` / [Models overview](https://platform.claude.com/docs/en/about-claude/models/overview).

**Recommendation for this product:** `claude-opus-5` for the course-structuring pass and for question generation (quality is the stated hard requirement, and generation is a one-time cost amortised across every retake); `claude-haiku-4-5` or `claude-sonnet-5` for the cheap verification/judge pass in §6. *Confidence: medium-high.*

**Two API behaviours that will bite you (from the same reference):**
- On Claude Opus 5, **thinking is on by default** — omitting the `thinking` parameter runs adaptive thinking, and `max_tokens` caps thinking *plus* response text together. Size `max_tokens` accordingly or responses truncate mid-JSON.
- **Assistant-turn prefills return 400** on Opus 5 / Sonnet 5 / the 4.6–4.8 family. Use structured outputs instead of the old "prefill with `{`" trick.
- `temperature`, `top_p`, `top_k` are **removed** on Opus 5 / Fable 5 / Opus 4.7+ — sending them returns 400.

---

## 2. Structured output for question generation

### 2.1 Anthropic

Mechanism: `output_config.format` with `{"type": "json_schema", "schema": {...}}`. The docs state validity is **guaranteed**, not best-effort:

> "Structured outputs guarantee schema-compliant responses through constrained decoding: **Always valid** — no more `JSON.parse()` errors; **Type safe** — guaranteed field types and required fields; **Reliable** — no retries needed for schema violations."
> — [Structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs)

**Supported:** all basic types; `enum` (strings/numbers/bools/nulls only); `const`; `anyOf`, `allOf` (`allOf` with `$ref` not supported); `$ref` / `$def` / `definitions` (internal only); `default`; `required`; `additionalProperties: false` (mandatory on objects); string `format` values `date-time`, `time`, `date`, `duration`, `email`, `hostname`, `uri`, `ipv4`, `ipv6`, `uuid`; array `minItems` — **only values 0 and 1**.

**Not supported:** recursive schemas; complex types inside `enum`; external `$ref`; `minimum` / `maximum` / `multipleOf`; `minLength` / `maxLength`; any array constraint beyond `minItems: 0|1`; `additionalProperties` set to anything other than `false`. Unsupported features return **400 with details**.

**No documented nesting-depth or schema-size limit** for Anthropic.

**Practical consequences for the MCQ schema:**
- You **cannot** express "exactly 4 choices" via `minItems: 4` / `maxItems: 4`. Either enumerate four named fields (`choice_a`…`choice_d`) or validate the array length in Go after the fact.
- You **cannot** bound stem length with `maxLength`. Enforce it in Go, and mirror the constraint in the field `description` (the docs note the SDK helpers do exactly this: they strip the unsupported constraint and fold it into the description).
- `question_type` as a string `enum` (`"conceptual" | "calculation"`) **is** supported — this is how you implement the `--force` calculation-only mode at the schema level.

**Go specifics:** the standard (non-beta) API takes a raw `map[string]any` schema; the beta API can reflect a Go struct. The docs suggest `invopop/jsonschema` with `Reflector{AllowAdditionalProperties: false, DoNotReference: true}` to generate the map from a struct.

**Performance:** first request with a new schema pays a grammar-compilation cost; compiled grammars are **cached for 24 hours**. Changing only `name`/`description` does **not** invalidate the grammar cache, but changing `output_config.format` **does** invalidate the prompt cache.

**Blocking incompatibility:** citations + structured outputs → **400**. See §6.3.

### 2.2 OpenAI

`json_schema` with `strict: true`. Per [Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs):

- **Supported:** String, Number, Boolean, Integer, Object, Array, Enum, `anyOf`; `pattern` and `format` on strings; `multipleOf`/`maximum`/`exclusiveMaximum`/`minimum`/`exclusiveMinimum` on numbers; `minItems`/`maxItems` on arrays; `$ref` **and recursion**.
- **Not supported:** `allOf`, `not`, `dependentRequired`, `dependentSchemas`, `if`, `then`, `else`.
- **Hard limits (the only provider that publishes them):** nesting depth **10**; up to **5,000 object properties** total; combined property-names/definitions/enums/consts string size **≤ 120,000 characters**; **≤ 1,000 enum values** total, and a single string enum with 250+ values is capped at 15,000 characters.
- **All fields must be `required`** — model optionality as `type: ["string","null"]`. `additionalProperties: false` is mandatory. Root-level objects **cannot** use `anyOf`.
- **Validity guarantee is conditional:** guaranteed *except* on refusals, `max_tokens` truncation, or content-filter interruption. Refusals surface in a dedicated `refusal` field which you must check.

Notably, OpenAI **does** support `minItems`/`maxItems` and `maxLength`, so "exactly 4 choices, stem ≤ 400 chars" is expressible there and is not on Anthropic.

### 2.3 Gemini

`response_format` with `mime_type: "application/json"` plus a `schema`, per [Structured output](https://ai.google.dev/gemini-api/docs/structured-output). Go equivalent: `GenerateContentConfig{ResponseMIMEType: "application/json", ResponseSchema: ...}` ([pkg.go.dev](https://pkg.go.dev/google.golang.org/genai)).

- **Supported subset:** `string`, `number`, `integer`, `boolean`, `object`, `array`, `null`; `title`, `description`; `properties`, `required`, `additionalProperties`; `enum` and `format` (`date-time`, `date`, `time`) on strings; `enum`, `minimum`, `maximum` on numbers; `items`, `prefixItems`, `minItems`, `maxItems` on arrays.
- **Limitations, quoted:** *"Not all JSON Schema features are supported"* and *"Very large or deeply nested schemas may be rejected"* — **with no numeric limits given**.
- **Weaker guarantee:** the guide says output is *"syntactically correct JSON"* but advises developers to validate — i.e. syntactic validity, not full semantic schema conformance.
- The structured-output guide gives Python/JS/REST examples but **no Go example**.

### 2.4 Verdict

Anthropic's guarantee wording is the strongest, OpenAI's constraint vocabulary is the richest, Gemini's is the weakest and least specified. **Regardless of provider, validate the parsed MCQ in Go before persisting** — schema conformance does not mean the item is *good* (see §6). *Confidence: high.*

---

## 3. Tool use for arithmetic

### 3.1 The Anthropic tool-use loop in Go

Two options, both from the [Go SDK reference](https://platform.claude.com/docs/en/api/sdks/go) / [tool use overview](https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview):

**Manual loop** — the shape you own end-to-end:

1. **Declare** the tool: `anthropic.ToolParam{Name, Description, InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{...}}}`, wrapped as `anthropic.ToolUnionParam{OfTool: &tool}` for the `Tools:` slice.
2. **Detect the turn:** check `resp.StopReason != anthropic.StopReasonToolUse` to exit the loop. Append `resp.ToParam()` to history **before** processing tool calls.
3. **Read the call:** type-switch `block.AsAny().(type)` on `anthropic.ToolUseBlock`; the arguments are raw JSON at `variant.JSON.Input.Raw()` — `json.Unmarshal` it, never string-match it.
4. **Feed results back:** `anthropic.NewToolResultBlock(block.ID, result, isError)`, all results for a turn collected into **one** `anthropic.NewUserMessage(toolResults...)`. Splitting parallel results across multiple user messages trains the model out of parallel calls.
5. On failure, return the `tool_result` with `is_error: true` — do not drop it.

**Tool runner (beta)** — `toolrunner.NewBetaToolFromJSONSchema(name, desc, fn)` + `client.Beta.Messages.NewToolRunner(...)` → `.RunToCompletion(ctx)`. It generates the schema from Go struct `jsonschema` tags and drives the loop, while still exposing per-turn hooks for approval/interception. Recommended for a custom-tool agent unless you need control it doesn't expose.

**Strict tool use:** set `Strict: anthropic.Bool(true)` on the tool definition (a top-level field, *not* on `tool_choice`), with `additionalProperties: false` supplied via `InputSchema.ExtraFields`. This guarantees `tool_use.input` validates exactly — worth using for a calculator whose inputs must be numeric.

### 3.2 Provider-native code execution

**Anthropic — `code_execution_20260521`** ([docs](https://platform.claude.com/docs/en/agents-and-tools/tool-use/code-execution-tool)):
- **GA on all current models** (Opus 5, Sonnet 5, Opus 4.5–4.8, Haiku 4.5, Fable/Mythos 5). All three tool versions (`_20250825`, `_20260120`, `_20260521`) are GA and **do not require a beta header**.
- Python pre-installed; Claude writes files with the file sub-tool and runs them with Bash.
- **No internet access in the container** — only pre-installed libraries; no runtime `pip install`.
- **Each request gets a new container** unless you pass a prior response's `container.id` back.
- `_20260120`+ adds REPL state persistence and programmatic tool calling; `_20260521` differs only in telling Claude about the **90-second wall-clock limit per Python cell** in programmatic tool calling (exceeding it yields a non-zero `return_code` and a `detection_timeout` status).
- **Free when combined with `web_search_20260209` / `web_fetch_20260209`**; otherwise standard code-execution pricing applies.

**Gemini code execution** ([docs](https://ai.google.dev/gemini-api/docs/code-execution)):
- Enabled with `{"type": "code_execution"}` in `tools`; Go type is `ToolCodeExecution`.
- Available on *"Gemini 3.5 Flash and newer models"*.
- 40+ libraries (NumPy, Pandas, SciPy-family, etc.).
- **Max runtime 30 seconds.** Model may regenerate code up to 5 times on error. Cannot return media artifacts.
- **No extra charge** — billed at standard token rates, with generated code and execution results counted as intermediate input tokens.
- Gemini 3 models support combining built-in tools with function calling.

### 3.3 Verdict — self-hosted calculator, not native code execution

**For a Go backend that persists results, a self-hosted calculator/expression-evaluator tool is the better fit.** Reasoning, grounded in the doc facts above:

| Factor | Native code execution | Self-hosted Go tool |
|---|---|---|
| Determinism at persist time | Model-authored Python; a re-run may take a different path | Same input → same output, always |
| Auditability | You store the model's code + stdout, both free-form | You store `{expression, result}` with a typed schema |
| Cost | Anthropic: hourly container billing unless paired with web search. Gemini: token-only. | Zero marginal cost |
| Latency | Container provision + up to 90s/cell (Anthropic) or 30s cap (Gemini) | Microseconds |
| Failure surface | Sandbox timeouts, `execution_time_exceeded`, no-internet surprises | In-process error you control |
| **Re-verification** | Cannot cheaply re-check a persisted question later | Can re-run the stored expression in a unit test / migration and assert the stored answer still holds |

That last row is decisive for this product: because questions are **persisted and reused**, you want the arithmetic to be re-checkable offline forever. Store the tool call and its result alongside the question, and add a CI job that re-evaluates every stored `computation` and fails if the persisted `correct_answer` no longer matches.

**Implementation sketch (not code — design only):** one tool, `evaluate`, with a strict schema `{expression: string, precision: integer}`, backed by a Go expression evaluator; system prompt instructs that *any* numeric value appearing in a question stem, in the correct answer, or in a distractor **must** come from an `evaluate` call. Then enforce it: if `question_type == "calculation"` and the turn produced zero `tool_use` blocks, discard the item. *Confidence: medium-high — the enforcement half is my design inference, not documented practice.*

**Caveat on the `--force` calculation-only mode:** the same reference notes that on Claude Opus 5, running with `thinking: {type: "disabled"}` can cause the model to write a tool call as **plain text instead of a `tool_use` block** — the turn succeeds, the tool never runs, and no error is raised. For a math-critical path, leave thinking on (it is on by default on Opus 5) and lower `effort` if you need to control cost.

---

## 4. PDF text extraction in Go

### 4.1 Native PDF to the model — the published limits

**Anthropic** ([PDF support](https://platform.claude.com/docs/en/build-with-claude/pdf-support)):

| Requirement | Limit |
|---|---|
| Max request size | **32 MB** (varies by platform) |
| Max pages per request | **600** (100 when the context window is under 1M tokens) |
| Format | Standard PDF, no passwords/encryption |

Both limits apply to the **entire request payload**. Three source options: `url`, `base64`, or Files API `file_id`. On Bedrock and Google Cloud, **only base64** is available.

Token cost — the two modes documented:
- **Text-extraction only:** cannot analyse images/charts/visual layout; *"approximately 1,000 tokens for a 3-page PDF"*.
- **Full visual mode:** processes each page as both text **and** image, understands charts/graphs/layouts; *"approximately 7,000 tokens for a 3-page PDF"* — i.e. **~2,300 tokens/page**.

The docs also warn: *"Dense PDFs (many small-font pages, complex tables, or heavy graphics) can fill the context window before reaching the page limit. Requests with large PDFs can also fail before reaching the page limit, even when using the Files API."*

**Gemini** ([Document processing](https://ai.google.dev/gemini-api/docs/document-processing)):
- **Up to 50 MB or 1000 pages**, inline or via Files API.
- Each page billed at **258 tokens**. Pages are rescaled to max 3072×3072 / min 768×768.
- Native vision on PDFs (text, images, diagrams, charts, tables). Non-PDF formats (TXT/MD/HTML) are extracted as pure text with no visual interpretation.
- Files API storage is free but **files are deleted after 48 hours**.

**Arithmetic for a realistic textbook.** A 400-page textbook at Anthropic's full-visual rate (~2,300 tok/page) is ~920K tokens — right at the 1M context ceiling, over the 600-page limit is not the binding constraint but cost is: at Opus 5 input pricing ($5/1M) that is **~$4.60 per ingest pass**, before any generation. At Gemini's 258 tok/page it is ~103K tokens — much cheaper, but you're then on a different provider for ingest than for generation.

**Conclusion: extract text in Go, chunk it, and send text.** Reserve native PDF for pages your extractor determines have no text layer. *Confidence: high.*

### 4.2 Go extraction libraries

| Library | Latest (proxy) | Licence | Text extraction? | Layout / tables | Scanned PDFs |
|---|---|---|---|---|---|
| [`github.com/ledongthuc/pdf`](https://github.com/ledongthuc/pdf) | `v0.0.0-20250511090121` (pseudo-version, no tags) | **BSD-3-Clause** (Go Authors copyright — it is a fork of `rsc.io/pdf`) | **Yes** — `GetPlainText()` and `GetStyledTexts()` | `GetStyledTexts()` returns per-sentence `Font`, `FontSize`, `X`, `Y` — enough to reconstruct reading order and detect headings, but **no table model** | **No** — text layer only |
| [`rsc.io/pdf`](https://pkg.go.dev/rsc.io/pdf) | **v0.1.1 (2018-04-11)** | BSD-3-Clause | Yes, minimal | None | No |
| [`github.com/pdfcpu/pdfcpu`](https://github.com/pdfcpu/pdfcpu) | **v0.13.0** (2026-06-09; v0.14.0-rc.1 pre-release announced) | **Apache-2.0** | **No.** `extract --mode content` emits **raw PDF content streams** (`BT`/`ET`, `Tf`, `Tm`, `Tj`, `cm`, `Do`), not readable text. Extract modes are images, fonts, content, pages, metadata — plain text is not among them. ([docs](https://pdfcpu.io/extract/extract_content)) | n/a | n/a |
| [`github.com/unidoc/unipdf/v4`](https://github.com/unidoc/unipdf) | **v4.11.0** (2026-07-03); README installs `/v5` | **UniDoc EULA — commercial, requires a licence key to operate.** *"This software package is a commercial product and requires a license code to operate."* ([LICENSE.md](https://github.com/unidoc/unipdf/blob/master/LICENSE.md), [EULA](https://unidoc.io/eula/)) | **Yes, best-in-class** — text with size/position/formatting, an explicit [PDF-to-CSV tabular extraction example](https://github.com/unidoc/unipdf-examples/blob/master/text/pdf_to_csv.go) | Yes — documented table extraction | Partially — has **CCITTFaxDecode** and **JBIG2 decoding**, i.e. it can decode scanned-fax images, but that is image decoding, **not OCR** |

**Licence call-out.** `unipdf` is **not** open source and **not** free for production. It is the only one of the four with real table extraction. Do not add it to `go.mod` without a purchased licence — this is a legal decision, not an engineering one. `pdfcpu` (Apache-2.0) and `ledongthuc/pdf` (BSD-3) are both safe for commercial use.

**Maintenance signal.** `rsc.io/pdf` has not been released since **2018** — treat as unmaintained; use the `ledongthuc` fork instead. `ledongthuc/pdf` has **no semver tags at all** (the proxy returns a pseudo-version), meaning you pin a commit hash; that's acceptable but worth knowing.

### 4.3 Calling out to a non-Go tool

If layout and table fidelity turn out to matter (likely for a textbook with worked examples in tables), the realistic options are shelling out to **`pdftotext -layout`** from [Poppler](https://poppler.freedesktop.org/) (GPL-2.0 — fine as a *separate process* you invoke, since you are not linking it) or **[MuPDF's `mutool draw -F text`](https://mupdf.readthedocs.io/)** (AGPL-3.0 or commercial — the AGPL reach on a subprocess is a question for a lawyer, not for me). *I did not verify these tools' current versions or flags against their docs in this pass — flagged in Open Questions.*

### 4.4 Scanned/image PDFs

**No pure-Go option does OCR.** All four libraries above operate on the PDF text layer. For a scanned upload, the text layer is empty or near-empty.

Recommended handling, in order of increasing effort:
1. **Detect and reject** — after extraction, if `len(text) / pageCount` is below a threshold (say 100 chars/page), return a clear 4xx: "this PDF appears to be scanned; we need a text-based PDF."
2. **Route the affected pages to the model as native PDF/images** — Anthropic's full-visual mode and Gemini both do vision on page images, which is effectively OCR-plus-comprehension. Cost is the ~2,300 tok/page (Anthropic) or 258 tok/page (Gemini) figure above, so this is viable for *some* pages, not a whole book.
3. Bolt on a real OCR service. Out of scope for v1.

*Confidence: high.*

---

## 5. RAG / retrieval in Postgres

### 5.1 pgvector — primary facts

Version **0.8.6**, supporting **Postgres 13+** ([README](https://github.com/pgvector/pgvector/blob/master/README.md)). Install paths: source build (Linux/Mac/Windows), Docker, Homebrew, PGXN, APT, Yum, pkg, APK, conda-forge; also preinstalled on many hosted providers. Enable with `CREATE EXTENSION IF NOT EXISTS vector`.

**Distance operators (vector type):**

| Operator | Distance |
|---|---|
| `<->` | L2 / Euclidean |
| `<#>` | negative inner product |
| `<=>` | cosine |
| `<+>` | L1 / taxicab |
| `<~>` / `<%>` | Hamming / Jaccard (binary vectors) |

**Index types:**

| | HNSW | IVFFlat |
|---|---|---|
| Query performance | Better speed-recall | Weaker speed-recall |
| Build time | Slower | Faster |
| Memory | Higher | Lower |
| Needs data first? | **No** — can build on an empty table | Yes — build after data exists |
| Build params | `m` (default 16), `ef_construction` (default 64) | `lists` — `rows/1000` under 1M rows, `sqrt(rows)` above |
| Query param | `hnsw.ef_search` (default 40) | `ivfflat.probes` (default 1) |

**HNSW is the right choice here** precisely because it can be created before any data exists — your migrations run at boot, and chunks arrive later.

**Dimension limits and storage:**

| Type | Max dims | Storage/vector | HNSW index limit | IVFFlat index limit |
|---|---|---|---|---|
| `vector` | 16,000 | 4×dims + 8 B | **2,000** | **2,000** |
| `halfvec` | 16,000 | 2×dims + 8 B | 4,000 | 4,000 |
| `bit` | 64,000 | dims/8 + 8 B | 64,000 | 64,000 |
| `sparsevec` | 16,000 non-zero | 8×nnz + 16 B | 1,000 nnz | — |

The **2,000-dimension index ceiling on `vector`** is the practical constraint: `text-embedding-3-large` is 3,072 dims natively and would need dimension reduction or `halfvec` to be indexable.

**Operator classes for indexes:** `vector_l2_ops`, `vector_ip_ops`, `vector_cosine_ops`.

### 5.2 Using it from Go / GORM

[`github.com/pgvector/pgvector-go`](https://github.com/pgvector/pgvector-go) — **v0.4.1** (2026-07-30, per the module proxy). Supports pgx, pg, Bun, Ent, **GORM**, and sqlx. The GORM path from its README:

```go
import "github.com/pgvector/pgvector-go"

db.Exec("CREATE EXTENSION IF NOT EXISTS vector")

type Item struct {
    Embedding pgvector.Vector `gorm:"type:vector(3)"`
}

db.Create(&Item{Embedding: pgvector.NewVector([]float32{1, 2, 3})})

var items []Item
db.Clauses(clause.OrderBy{
    Expression: clause.Expr{SQL: "embedding <-> ?", Vars: []interface{}{pgvector.NewVector([]float32{1, 1, 1})}},
}).Limit(5).Find(&items)

db.Exec("CREATE INDEX ON items USING hnsw (embedding vector_cosine_ops)")
```

This fits your existing `gorm.io/gorm` + `gorm.io/driver/postgres` stack with **no driver change** — GORM's Postgres driver already uses pgx v5 underneath (`github.com/jackc/pgx/v5 v5.6.0` is in your `go.mod`). Index DDL goes in a golang-migrate `.up.sql`, not in the Go code.

The repo also ships a [hybrid-search example using Reciprocal Rank Fusion](https://github.com/pgvector/pgvector-go/blob/master/examples/hybrid/main.go).

### 5.3 Postgres full-text search — the cheaper alternative

Fully documented in [PostgreSQL: Controlling Text Search](https://www.postgresql.org/docs/current/textsearch-controls.html). The pieces that matter:

- **`to_tsvector(config, doc)`** — tokenise, lemmatise, strip stop words, keep positions. Returns NULL on NULL input, so wrap columns in `coalesce()`.
- **`setweight(tsvector, 'A'|'B'|'C'|'D')`** — weight fields differently. For your model: lesson title `'A'`, chunk body `'D'`.
- **Query parsers:** `to_tsquery` (operators `&`, `|`, `!`, `<->`), `plainto_tsquery` (ANDs everything), `phraseto_tsquery` (phrase), and **`websearch_to_tsquery`** — the one to use for user-typed scoping prompts, because it accepts web-search syntax (`"quoted phrase"`, `OR`, leading `-` for NOT) and **never raises a syntax error**.
- **Ranking:** `ts_rank` (frequency) and `ts_rank_cd` (cover density — rewards proximity). Default weights `{0.1, 0.2, 0.4, 1.0}` = `{D, C, B, A}`. Normalisation is a bitmask; `32` (`rank/(rank+1)`) scales to 0–1 but the docs note it is **cosmetic — it does not change ordering**.
- **Documented ranking limitations:** no global corpus statistics (so no meaningful percentage scores); ranking is *"expensive and I/O-bound"* since it must read each matching document's `tsvector`; `ts_rank_cd` returns zero on stripped (position-less) lexemes; the built-in functions are explicitly described as **examples only**.
- **`ts_headline`** gives you highlighted snippets — useful for showing the student *where* a question came from. **XSS warning in the docs:** its output is not HTML-safe; sanitise it.

### 5.4 Embedding cost and provider

| Provider / model | $/1M tokens | Source |
|---|---|---|
| OpenAI `text-embedding-3-small` | **$0.02** | [OpenAI pricing](https://developers.openai.com/api/docs/pricing) |
| OpenAI `text-embedding-3-large` | $0.13 | same |
| Gemini Embedding (text-only) | $0.15 (batch $0.075) | [Gemini pricing](https://ai.google.dev/gemini-api/docs/pricing) |
| Gemini Embedding 2 (multimodal) | $0.20 text / $0.45 image (batch $0.10 / $0.225) | same |
| Voyage `voyage-4-lite` | $0.02 (200M free tokens) | [Voyage pricing](https://docs.voyageai.com/docs/pricing) |
| Voyage `voyage-4` | $0.06 (200M free tokens) | same |
| Voyage `rerank-2.5-lite` | $0.02 (200M free tokens) | same |

**Anthropic has no first-party embeddings endpoint** — Voyage is the partner path, and Voyage's 200M free-token allowance means embedding your entire corpus is likely free for a long time.

**Which Go SDK produces embeddings:** `google.golang.org/genai` via `Models.EmbedContent` / `Batches.CreateEmbeddings`; `github.com/openai/openai-go/v3` via its embeddings resource. Voyage has **no official Go SDK** — you would call its REST API directly. *This last point I did not verify against Voyage's docs in this pass — see Open Questions.*

### 5.5 Verdict

**Start with FTS.** Your retrieval unit is "chunks of one textbook the user just uploaded" — a corpus of hundreds of chunks, not millions. `websearch_to_tsquery` + `ts_rank_cd` over a GIN-indexed `tsvector` column requires **zero new dependencies, zero new infrastructure, and zero per-token cost**, and it handles the "scope questions to this part of the content" prompt option directly. Add pgvector + HNSW when you have evidence that lexical matching misses paraphrased concepts — and when you do, keep FTS and fuse with RRF as pgvector's own example does. *Confidence: medium-high — this is a judgement call, and a large multi-book corpus would change it.*

---

## 6. Grounding and question-quality techniques

### 6.1 Documented practice (provider docs — authoritative)

From Anthropic's [Reduce hallucinations](https://platform.claude.com/docs/en/test-and-evaluate/strengthen-guardrails/reduce-hallucinations) guide, all four techniques are officially documented, with exact prompt patterns:

1. **Allow "I don't know."** *"Explicitly give Claude permission to admit uncertainty. This simple technique can drastically reduce false information."* For your use case: allow the generator to return **fewer** questions than requested rather than padding with vague ones. This directly attacks the stated pain point.
2. **Extract quotes before answering.** *"For tasks involving long documents (>20k tokens), ask Claude to extract word-for-word quotes first before performing its task."* The documented two-step prompt shape: (1) extract exact quotes relevant to X, or state "No relevant quotes found"; (2) *"Only base your analysis on the extracted quotes."*
3. **Verify with citations, and retract unsupported claims.** The documented pattern: *"After drafting, review each claim… For each claim, find a direct quote from the documents that supports it. If you can't find a supporting quote for a claim, remove that claim."* Mapped to MCQs: after generating an item, require a supporting span for the stem *and* for why the correct answer is correct; drop items that fail.
4. **Advanced:** chain-of-thought verification; **best-of-N verification** (*"Run Claude through the same prompt multiple times and compare the outputs. Inconsistencies across outputs could indicate hallucinations"*); iterative refinement; **external knowledge restriction** (*"explicitly instruct Claude to only use information from provided documents and not its general knowledge"*).

The guide closes with an explicit caveat: these techniques *"significantly reduce hallucinations, they don't eliminate them entirely."*

### 6.2 Published research (arXiv — verified titles/IDs)

| Paper | arXiv ID | Relevance |
|---|---|---|
| **D-GEN: Automatic Distractor Generation and Evaluation for Reliable Assessment of Generative Model** — Grace Byun, Jinho D. Choi; 2025-04-18 (v2 2025-06-12); ACL 2025 Findings | [2504.13439](https://arxiv.org/abs/2504.13439) | Two **automated** distractor-quality metrics you can actually implement: **ranking alignment** (do generated distractors preserve the discriminative power of ground-truth ones?) and **entropy analysis** (does model confidence over the options match the reference distribution?). Reports Spearman ρ 0.99 / Kendall τ 0.94 for ranking consistency, plus human eval on fluency, coherence, distractiveness, incorrectness. |
| **Harnessing Structured Knowledge: A Concept Map-Based Approach for High-Quality Multiple Choice Question Generation with Effective Distractors** — Scaria, Kennedy, Seth, Thakur, Subramani; 2025-05-02 | [2505.02850](https://arxiv.org/abs/2505.02850) | Directly maps onto your two-pass architecture. Builds a **hierarchical concept map** first (≈ your "AI pass #1 splits content into lessons/topics"), retrieves relevant sections as context, generates, then validates automatically. Reports **75.20%** of items meeting quality criteria vs ~37% baseline, and a **guess success rate of 28.05% vs 37.10%** baseline — i.e. items that actually test understanding rather than being guessable. |
| **Judging LLM-as-a-Judge with MT-Bench and Chatbot Arena** — Zheng et al.; 2023-06-09 (rev. 2023-12-24); NeurIPS 2023 D&B | [2306.05685](https://arxiv.org/abs/2306.05685) | The canonical LLM-as-judge reference. Strong judges reach *"over 80% agreement"* with human preferences — matching inter-human agreement. **Also names the failure modes you must design around: position bias, verbosity bias, self-enhancement bias, and limited reasoning ability.** |
| **Self-Consistency Improves Chain of Thought Reasoning in Language Models** — Wang, Wei, Schuurmans, Le, Chi, Narang, Chowdhery, Zhou; 2022-03-21 (rev. 2023-03-07) | [2203.11171](https://arxiv.org/abs/2203.11171) | Sample multiple reasoning paths, take the majority answer. +17.9% on GSM8K, +12.2% on AQuA. This is the principled version of "answer the question N times and check the answers agree". |
| **PAL: Program-aided Language Models** — Gao, Madaan, Zhou, Alon, Liu, Yang, Callan, Neubig; 2022-11-18 (rev. 2023-01-27) | [2211.10435](https://arxiv.org/abs/2211.10435) | The research basis for §3's recommendation: the LM decomposes the problem but *"offloads the solution step to a runtime such as a Python interpreter."* Neural comprehension, symbolic computation. |
| **Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks** — Lewis et al.; 2020-05-22 (rev. 2021-04-12); NeurIPS 2020 | [2005.11401](https://arxiv.org/abs/2005.11401) | Origin of RAG. Cited for completeness. |

Other candidate papers surfaced but **not individually verified** (title/abstract not fetched): [2508.20217](https://arxiv.org/abs/2508.20217) (prompting strategies for K-12 item generation), [2404.02124](https://arxiv.org/abs/2404.02124) (math MCQ distractor generation), [2406.19356](https://arxiv.org/abs/2406.19356) (DiVERT), [2501.13125](https://arxiv.org/abs/2501.13125) (distractors via student-choice prediction). Treat these as leads, not citations.

### 6.3 Requiring a citation/span back to the source chunk — the blocking constraint

Anthropic's [Citations](https://platform.claude.com/docs/en/build-with-claude/citations) feature would be the obvious mechanism. It gives you `cited_text` with location types `char_location` (`start_char_index`/`end_char_index`), `page_location` (1-indexed page numbers), and `content_block_location` (0-indexed block-index range), and the docs state *"citations are guaranteed to contain valid pointers to the provided documents"* because the API extracts `cited_text` itself. Cost is favourable: `cited_text` **does not count toward output tokens**, and does not count toward input tokens when passed back in later turns; only a slight input-token increase for the system-prompt additions and chunking.

**But you cannot use it here:**

> "Citations cannot be used together with structured outputs. If you enable citations on any user-provided document (`document` blocks or `search_result` blocks) and also include the `output_config.format` parameter… the API returns a **400 error**. This is because citations require interleaving citation blocks with text output, which is incompatible with the strict JSON schema constraints of structured outputs."
> — [Citations](https://platform.claude.com/docs/en/build-with-claude/citations)

Since MCQ generation needs structured output, **you must implement provenance yourself.** The workable design:

- Add `source_quote: string` and `source_chunk_id: string` to the MCQ schema, required fields.
- **Verify in Go**: `strings.Contains(chunk.Text, item.SourceQuote)` after whitespace normalisation. If the quote is not a verbatim substring of the cited chunk, reject the item. This gives you the same guarantee the citations API would have, enforced on your side.
- Persist `source_chunk_id` + character offsets so you can render "this question came from page 214" and re-verify later.

Also useful from the same doc: for RAG specifically, *"if you want Claude to be able to cite specific sentences from your RAG chunks, you should put each RAG chunk into a plain text document"* (auto-chunked into sentences), versus custom-content documents which are used as-is with no further chunking.

### 6.4 Verifying a generated question — the recommended pipeline

Combining the documented practice (§6.1), the papers (§6.2), and your persistence model:

1. **Generate** with structured output + `source_quote` per item, at high effort, with an explicit instruction to return fewer items rather than weak ones.
2. **Mechanical gates in Go (cheap, deterministic, run first):** exactly N choices; all choices distinct after normalisation; exactly one marked correct; no choice is a substring of another; stem length within bounds; no "all/none of the above" unless explicitly allowed; `source_quote` is a verbatim substring of the cited chunk; for `calculation` items, a tool call produced the answer.
3. **Blind-answer check (the strongest signal, and cheap):** re-ask the model the question **with the source text withheld and the correct-answer label hidden**, N times (self-consistency, [2203.11171](https://arxiv.org/abs/2203.11171)). Two failure modes it catches:
   - The model answers correctly every time **without** the source → the item is answerable from general knowledge or is given away by the distractors. That is the "guess success rate" metric from [2505.02850](https://arxiv.org/abs/2505.02850). Reject or flag.
   - The model's answers **disagree across samples** or disagree with the labelled key → the item is ambiguous or the key is wrong. This is the direct instrument against "vague, uninterpretable questions."
4. **LLM-as-judge rubric pass** ([2306.05685](https://arxiv.org/abs/2306.05685)) on the survivors, scoring: stem self-containedness, single unambiguous correct answer, distractor plausibility, distractor homogeneity (same category/length/grammatical form), and groundedness in the quoted span. **Mitigate the documented biases**: randomise choice order before judging (position bias), and prefer a *different* model as judge than the generator (self-enhancement bias). Verbosity bias is why you also cap stem length mechanically.
5. **Persist only what survives**, storing the judge score and the blind-answer statistics as columns so you can tune thresholds later without regenerating.

**What is documented vs. what is my inference:**
- *Documented:* allow-uncertainty, quote-first, retract-unsupported-claims, best-of-N, external-knowledge-restriction (Anthropic docs); LLM-judge agreement rates and bias taxonomy ([2306.05685](https://arxiv.org/abs/2306.05685)); self-consistency majority voting ([2203.11171](https://arxiv.org/abs/2203.11171)); guess-success-rate and concept-map-first generation ([2505.02850](https://arxiv.org/abs/2505.02850)); ranking-alignment and entropy metrics for distractors ([2504.13439](https://arxiv.org/abs/2504.13439)); offloading computation to a runtime ([2211.10435](https://arxiv.org/abs/2211.10435)).
- *My inference / speculation:* the specific ordering of the five stages; the exact mechanical gate list; using the blind-answer check specifically as an admission gate before DB insert; persisting judge scores as tunable columns. These are reasonable but **not** citable practice.

---

## 7. Async job handling in Go

| | [River](https://riverqueue.com/docs) | [asynq](https://github.com/hibiken/asynq) | Hand-rolled `SKIP LOCKED` |
|---|---|---|---|
| Latest version | **v0.42.0** (2026-07-31) | **v0.26.0** (2026-02-03) | n/a |
| Backing store | **Postgres** | **Redis 4.0+** | Postgres |
| Licence | **MPL-2.0** | **MIT** | yours |
| New infra required | **None** — uses your existing DB | **Redis** | None |
| Driver | `riverpgxv5` (pgx v5, primary) or `riverdatabasesql` (`database/sql`) | Redis client | pgx / GORM raw |
| Postgres support | "three most recent major versions" | n/a | any |
| Maintainer stability statement | Not found in the docs I fetched | *"The library relatively stable and is currently undergoing **moderate development** with less frequent breaking API changes."* Also: *"The public API could change without a major version update before `v1.0.0` release."* | n/a |
| Activity (GitHub API, 2026-08-01) | last push **2026-07-31**, 5,506 stars, not archived | last push **2026-06-22**, latest release **2026-02-03**, 290 open issues, 13,581 stars, not archived | n/a |

**River details** ([docs](https://riverqueue.com/docs)): two-type pattern per job kind — a `JobArgs` struct (JSON-serialised parameters, identified by a `Kind()` string) and a generic `Worker[T]`. Setup is: `go get github.com/riverqueue/river` + `go install github.com/riverqueue/river/cmd/river@latest`, run the CLI migrations (which create the tables and enable leader election), register workers, start the client. Documented features: **transactional enqueuing** (insert the job inside the same DB transaction as your business write — no dual-write race), **`SKIP LOCKED`** for efficient fetching, graceful shutdown with configurable stop modes, and insert-only clients for services that enqueue but don't process.

**asynq features** ([README](https://github.com/hibiken/asynq/blob/master/README.md)): at-least-once execution, scheduling and retries, crash recovery, weighted/strict priority queues, de-duplication, timeouts/deadlines, task aggregation, handler middleware, queue pausing, periodic tasks, Redis Sentinel HA, Prometheus metrics, plus the Asynqmon web UI and CLI. Supports the last two Go versions.

**Recommendation: River.** Three reasons, in order:
1. **Transactional enqueue.** Your ingest flow is "insert a `documents` row, then enqueue a parse job." With Redis those are two systems and you can lose the job or orphan the row. With River they are one Postgres transaction. This is the single strongest technical argument.
2. **No new infrastructure.** You already run Postgres and pgx v5. Adding Redis means another service to deploy, monitor, back up, and secure — for a project with no auth yet, that is premature.
3. **Release cadence.** River shipped 2026-07-31; asynq's last release was 2026-02-03 with 290 open issues. Neither is abandoned, but River is moving faster.

**Caveats to raise before committing:**
- **River is MPL-2.0, not MIT/Apache.** MPL is file-level weak copyleft — using it as an unmodified library dependency is unproblematic for a closed-source app, but if you *modify* River's files you must publish those files. Confirm this is acceptable.
- Both are **pre-1.0** (`v0.42.0` / `v0.26.0`). Pin exact versions.
- **Hand-rolling `SELECT ... FOR UPDATE SKIP LOCKED`** is genuinely viable for one job type and ~50 lines, but you would then re-implement retries with backoff, visibility timeouts, dead-lettering, leader election for periodic jobs, and graceful shutdown. For a multi-minute PDF-ingest job that must survive restarts, that is not the place to save a dependency. Choose it only if the MPL licence is a blocker.

**Longer-term fit:** the "finish this in N days → AI plans the course" feature is a **scheduled** job. River's docs mention periodic jobs via leader election; asynq lists periodic task scheduling explicitly. Both cover it. *Confidence: medium-high.*

---

## 8. Schema implications — my own analysis of this repo

> **This section is my reading of the code at `E:\contribute\teach-me-all\backend`, not external research.** Every claim below is traceable to a file in the repo.

### 8.1 Current schema, as written

All six migrations are pure SQL under `backend/migrations`:

| Migration | Table | Columns |
|---|---|---|
| `0001_create_users.up.sql` | `users` | `id UUID PK DEFAULT gen_random_uuid()`, `email varchar(100) UNIQUE`. Also `CREATE EXTENSION IF NOT EXISTS pgcrypto`. |
| `0002_create_courses.up.sql` | `courses` | `id`, `title VARCHAR(100)`, `is_public BOOLEAN`, `user_id UUID` FK → `users` ON DELETE CASCADE |
| `0003_create_lessons.up.sql` | `lessons` | `id`, `title VARCHAR(100)`, `course_id UUID` FK → `courses` ON DELETE CASCADE |
| `0004_create_exams.up.sql` | `exams` | `id`, `title VARCHAR(100)`, `lesson_id UUID` FK → `lessons` ON DELETE CASCADE |
| `0005_create_questions.up.sql` | `questions` | `id`, `content VARCHAR(100)`, `exam_id UUID` FK → `exams` ON DELETE CASCADE |
| `0006_create_choice.up.sql` | `choices` | `id`, `content VARCHAR(100)`, `question_id UUID` FK → `questions` ON DELETE CASCADE |

### 8.2 Correctness bugs (not just gaps) — fix these regardless of the AI feature

1. **`models.Question.IsCorrect` does not exist in the database.**
   `backend/internal/models/question.go` declares:
   ```go
   IsCorrect bool `gorm:"default:false"`
   ```
   but `migrations/0005_create_questions.up.sql` has no `is_correct` column, and nothing calls `AutoMigrate`. Because `questionRepository.GetQuestionByExamsID` uses `db.Where(...).Find(&questions)`, GORM issues `SELECT *` — so there is **no SQL error**; the field silently stays `false` on every row, forever. `dto.QuestionRespone` then serialises it as `"isCorrect": false` to the client. **A silently-always-false field is worse than a missing one.**

2. **`is_correct` is on the wrong entity.** For an MCQ, correctness is a property of a *choice*, not of a *question*. The `choices` table has no `is_correct` column at all, so **the answer key is unrepresentable**. Nothing in the current schema can express "which option is right."

3. **Answer key would leak to the client.** `dto.QuestionRespone` exposes `isCorrect` on the question payload returned by `GET /api/exams/:id/questions`. Once correctness is real, this endpoint hands the student the answers. Sit-the-exam and review-the-exam need different DTOs.

4. **`dto.ExamWithQuestions` has a wrong JSON tag** (`backend/internal/dto/exam_dto.go`): `Title string \`json:"content"\`` — the exam title serialises under the key `content`.

5. **Five of six migrations have no `.down.sql`.** Only `0001_create_users.down.sql` exists. `migrate.Up()` works, but rollback is impossible from `0002` onward — a real problem the first time an AI-related migration goes wrong in production.

6. **`file://migrations` is CWD-relative.** `database.RunMigrate` uses `migrate.New("file://migrations", dsn)`, so the binary only works when launched from `backend/`. Fine now; a footgun once this is containerised.

7. **Nullability and index tags disagree with the SQL.** Every GORM model tags fields `not null` and FK columns `index`, but the migrations declare all of them nullable with no indexes. Postgres does **not** auto-create an index for a foreign key column, so `WHERE course_id = ?`, `WHERE lesson_id = ?`, and `WHERE exam_id = ?` — the three queries the existing repositories actually run — are sequential scans. Add `CREATE INDEX` statements and `NOT NULL` to the migrations, or switch to `AutoMigrate` (not recommended alongside golang-migrate).

8. **`courseRepository.GetCourseByID` is an N+1 query** — one `SELECT` for lessons, then one per lesson for exams. Not urgent at current scale, but it will be the first thing to hurt once a course has 40 lessons.

### 8.3 What the schema cannot express for this product

| Product requirement | Blocked by |
|---|---|
| A question stem of realistic length | **`questions.content VARCHAR(100)`.** 100 characters is roughly one short sentence. A grounded calculation question is easily 300–600 characters. **This single constraint makes the feature impossible as specified.** Use `TEXT`. |
| A realistic answer option | **`choices.content VARCHAR(100)`.** Same problem, same fix. |
| The answer key | No `is_correct` on `choices` (see §8.2). Needs `is_correct BOOLEAN NOT NULL DEFAULT false` plus a partial unique index to enforce exactly one correct choice per question. |
| Stable question / choice ordering | **No ordering column anywhere.** `Find(&questions)` returns rows in whatever order Postgres chooses; two students see the same exam in different orders, and a retake doesn't match the original. Needs `position INT NOT NULL` (or `sort_order`) on `lessons`, `questions`, and `choices`, with a unique index on `(parent_id, position)`. |
| Uploaded PDFs | **No `documents` table.** Nowhere to record filename, storage path/key, SHA-256, page count, byte size, MIME type, upload status, extraction status, or error message. |
| RAG chunks | **No `document_chunks` table.** Nowhere for chunk text, page range, char offsets, `tsvector`, or (later) `vector` embedding. |
| Provenance / "where did this question come from" | **No source columns on `questions`.** Needs at minimum `source_chunk_id UUID`, `source_quote TEXT`, and page/offset. Required by §6.3, since the citations API is unusable alongside structured outputs. |
| Retaking exams / attempt history | **No `exam_attempts` / `attempt_answers` tables.** "Users can retake past exams" is a stated requirement and is completely unrepresentable. Needs attempts (user, exam, started/submitted timestamps, score) and per-question answers (attempt, question, chosen choice, correctness, time spent). |
| Calculation-only questions (`--force`) | No `question_type` column, no place for the tool call / expression / numeric answer / tolerance / units. Needs `question_type` (enum or CHECK-constrained text) plus a `computation JSONB` or a sibling table. |
| Explaining a wrong answer to the student | No `explanation` column on `questions` (or per-choice rationale on `choices`). Standard for an e-learning product and free to generate in the same LLM call. |
| Quality gating / re-review | No columns for judge score, blind-answer statistics, or review state — so §6.4's thresholds can't be tuned without regenerating everything. Needs `quality_score`, `review_status`, `generated_by_model`, `generated_at`. |
| Difficulty / Bloom level / topic tagging | No `difficulty`, no `bloom_level`, no topic tags. The "key topics" half of AI pass #1 has nowhere to live — `lessons` is a flat list with only a title. |
| Async ingest jobs | No jobs table. River creates its own (via its CLI migrations), so this is solved by adopting River rather than by hand-writing DDL. |
| Course scheduling (future) | No `study_plan` / `plan_items` tables, no dates. Explicitly out of scope for now but worth knowing the schema has no hook for it. |
| Auditability | **No `created_at` / `updated_at` on any table**, and no soft delete. For content that is generated once and reused indefinitely, "when was this generated and by which model" is not optional. |
| Ownership enforcement | `courses.is_public` and `courses.user_id` exist but **no auth exists**, and no handler checks either. Every course is readable by anyone who guesses a UUID. Not a schema gap per se, but the schema implies an authorisation model that isn't enforced anywhere. |

### 8.4 Minimum schema work before any AI code is written

In dependency order, as new golang-migrate pairs (**each with a `.down.sql`**):

1. `ALTER TABLE questions ALTER COLUMN content TYPE TEXT;` and the same for `choices.content`.
2. `ALTER TABLE choices ADD COLUMN is_correct BOOLEAN NOT NULL DEFAULT false;` + `CREATE UNIQUE INDEX ... ON choices (question_id) WHERE is_correct;` — remove `IsCorrect` from `models.Question` in the same change.
3. Add `position INT NOT NULL DEFAULT 0` to `lessons`, `questions`, `choices` with `(parent_id, position)` unique indexes; add `ORDER BY position` to every repository query.
4. Add `created_at TIMESTAMPTZ NOT NULL DEFAULT now()` / `updated_at` to all tables.
5. Add indexes on `courses.user_id`, `lessons.course_id`, `exams.lesson_id`, `questions.exam_id`, `choices.question_id`; add `NOT NULL` to those FK columns.
6. New tables: `documents`, `document_chunks` (with a `tsvector` GENERATED column + GIN index), `exam_attempts`, `attempt_answers`.
7. New columns on `questions`: `question_type`, `explanation`, `source_chunk_id`, `source_quote`, `computation JSONB`, `quality_score`, `generated_by_model`, `generated_at`.
8. Backfill the five missing `.down.sql` files.

---

## Open questions / could not verify

1. **Anthropic Go SDK SemVer policy.** The README's stability posture is inferred from the absence of a warning plus the v1.61.0 major version. I grepped for a "Semantic versioning" section and **found none** — unlike `openai-go`, which has one. If the exact SemVer commitment matters, check [platform.claude.com/docs/en/api/sdks/go](https://platform.claude.com/docs/en/api/sdks/go), which I did not fetch.
2. **Gemini SDK stability.** No explicit GA/stability statement exists in the go-genai README or on pkg.go.dev beyond the v1.x version number and the announced v2.0.0 breaking change to `GenerateVideos`. The Go minimum version is rendered as a badge and could not be read as text.
3. **Gemini's structured-output limits are unquantified.** The docs say deeply nested schemas "may be rejected" but publish no depth or property-count numbers. If you go with Gemini you will have to discover these empirically.
4. **Gemini model naming looks unstable.** The code-execution doc references *"Gemini 3.5 Flash and newer"* and `gemini-3.6-flash`; the pricing page lists *Gemini 3.6 Flash* and *Gemini 2.5 Flash-Lite*; the pkg.go.dev example uses `gemini-2.5-flash`. Verify the exact current model ID before writing code.
5. **Anthropic code-execution pricing.** The doc says code execution is free when paired with `web_search_20260209`/`web_fetch_20260209` and that "standard code execution pricing applies" otherwise, but I **did not locate the actual dollar figure** in the fetched content. Check the [pricing page](https://platform.claude.com/docs/en/pricing) before relying on it.
6. **Anthropic model pricing is cached, not live.** The table in §1.3 comes from the bundled `claude-api` skill reference cached **2026-06-24** — five weeks stale as of today. Confirm against [platform.claude.com/docs/en/pricing](https://platform.claude.com/docs/en/pricing).
7. **Voyage AI has no Go SDK** — asserted from general knowledge, **not verified** against Voyage's docs in this pass. Verify before planning an embeddings integration on Anthropic's recommended provider.
8. **Poppler / MuPDF versions and flags were not verified.** §4.3 names them as options but I did not fetch their docs. The AGPL implications of invoking `mutool` as a subprocess are a legal question I am not qualified to answer.
9. **Four arXiv papers were surfaced by search but not individually verified** (2508.20217, 2404.02124, 2406.19356, 2501.13125). Do not cite them without fetching the abstracts.
10. **The pdfcpu "extract" documentation I fetched did not enumerate every mode exhaustively** — it listed images, fonts, content, pages, metadata. I am confident plain-text extraction is absent (the `--mode content` output is demonstrably raw PDF operators), but if you need certainty check the [pdfcpu CLI reference](https://pdfcpu.io/).
11. **`ledongthuc/pdf` extraction quality on real textbooks is untested.** Its two-column, table, and ligature behaviour is not documented anywhere I could find. **Benchmark it against three representative PDFs before committing** — this is the highest-risk unverified assumption in the whole document, because the entire ingest pipeline depends on it.
12. **River's own stability statement** was not found. I fetched [riverqueue.com/docs](https://riverqueue.com/docs) and the repo LICENSE (MPL-2.0), but the root `README.md` returned 404 from the raw endpoint and I read `docs/README.md` instead. Pre-1.0 status is inferred from the `v0.42.0` version string.
13. **OpenAI model lineup** (`gpt-5.6-sol`/`terra`/`luna`) is reported as returned by the pricing page; I did not cross-check against the models reference.
14. **Fiber v3 + River/asynq integration** was not researched — specifically how to share a pgx pool between GORM and River's `riverpgxv5` driver. Your `go.mod` has `jackc/pgx/v5 v5.6.0` as an indirect dependency via GORM's Postgres driver; whether you can reuse that pool or need a second one is unverified.
