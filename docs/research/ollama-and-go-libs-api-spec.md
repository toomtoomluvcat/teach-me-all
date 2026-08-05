# API spec — verified against primary sources (2026-08-02)

Sources are inline per claim. Everything below was read from the actual doc/source
file, not recalled. Anything not confirmed is in the final UNVERIFIED list.

---

## 1. Ollama HTTP API

Primary: `https://github.com/ollama/ollama/blob/main/docs/api.md` (raw:
`https://raw.githubusercontent.com/ollama/ollama/main/docs/api.md`)
Go types: `https://raw.githubusercontent.com/ollama/ollama/main/api/types.go`
Options table: `https://raw.githubusercontent.com/ollama/ollama/main/docs/modelfile.mdx`
keep_alive: `https://raw.githubusercontent.com/ollama/ollama/main/docs/faq.mdx`

### 1.1 `POST /api/chat` — parameters (verbatim from api.md)

- `model` (required) — model name
- `messages` — array
- `tools` — list of tools in JSON
- `think` — bool or `"low"` / `"medium"` / `"high"` / `"max"`

Message object fields:
- `role` — `system` | `user` | `assistant` | `tool`
- `content` — string
- `thinking` — string (thinking models)
- `images` (optional) — list of images (multimodal, e.g. `llava`)
- `tool_calls` (optional)
- `tool_name` (optional)

Advanced (optional):
- `format` — "the format to return a response in. Format can be `json` or a JSON schema."
- `options` — model params (see 1.5)
- `stream` — bool; `false` ⇒ single response object
- `keep_alive` — default `5m`

Go struct (api/types.go, `ChatRequest`) — authoritative types:

```go
Format    json.RawMessage `json:"format,omitempty"`
KeepAlive *Duration       `json:"keep_alive,omitempty"`
Options   map[string]any  `json:"options"`
Stream    *bool           `json:"stream,omitempty"`
```
`Message`:
```go
Content   string      `json:"content"`
Thinking  string      `json:"thinking,omitempty"`
Images    []ImageData `json:"images,omitempty"`   // type ImageData []byte
ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
ToolName  string      `json:"tool_name,omitempty"`
```

**`format` accepts BOTH**: the string `"json"` *or* a raw JSON Schema object.
Confirmed by `Format json.RawMessage` (types.go:144) + api.md line 511
("Format can be `json` or a JSON schema") + the structured-outputs example below.

### 1.2 Structured outputs — complete concrete example (verbatim, api.md L777)

```shell
curl -X POST http://localhost:11434/api/chat -H "Content-Type: application/json" -d '{
  "model": "llama3.1",
  "messages": [{"role": "user", "content": "Ollama is 22 years old and busy saving the world. Return a JSON object with the age and availability."}],
  "stream": false,
  "format": {
    "type": "object",
    "properties": {
      "age": {
        "type": "integer"
      },
      "available": {
        "type": "boolean"
      }
    },
    "required": [
      "age",
      "available"
    ]
  },
  "options": {
    "temperature": 0
  }
}'
```

Response (verbatim, api.md L805):

```json
{
  "model": "llama3.1",
  "created_at": "2024-12-06T00:46:58.265747Z",
  "message": {
    "role": "assistant",
    "content": "{\"age\": 22, \"available\": false}"
  },
  "done_reason": "stop",
  "done": true,
  "total_duration": 2254970291,
  "load_duration": 574751416,
  "prompt_eval_count": 34,
  "prompt_eval_duration": 1502000000,
  "eval_count": 12,
  "eval_duration": 175000000
}
```

**The content string lives at `.message.content` and is a JSON *string* you must
`json.Unmarshal` a second time.** Note: `format` is the schema object itself —
NOT wrapped in `{"type":"json_schema","json_schema":{...}}` like OpenAI.

Go request type:

```go
type chatReq struct {
	Model    string          `json:"model"`
	Messages []msg           `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   json.RawMessage `json:"format,omitempty"` // raw JSON Schema, or []byte(`"json"`)
	Options  map[string]any  `json:"options,omitempty"`
	KeepAlive any            `json:"keep_alive,omitempty"`
}
type chatResp struct {
	Model      string `json:"model"`
	CreatedAt  string `json:"created_at"`
	Message    struct {
		Role     string `json:"role"`
		Content  string `json:"content"`
		Thinking string `json:"thinking"`
	} `json:"message"`
	DoneReason string `json:"done_reason"`
	Done       bool   `json:"done"`
	TotalDuration   int64 `json:"total_duration"`   // nanoseconds
	LoadDuration    int64 `json:"load_duration"`
	PromptEvalCount int   `json:"prompt_eval_count"`
	EvalCount       int   `json:"eval_count"`
}
```
(api.md L29: "All durations are returned in nanoseconds.")

### 1.3 Vision / images

api.md L963 verbatim: *"Send a chat message with images. The images should be
provided as an array, with the individual images encoded in Base64."*

```shell
curl http://localhost:11434/api/chat -d '{
  "model": "llava",
  "messages": [
    {
      "role": "user",
      "content": "what is in this image?",
      "images": ["iVBORw0KGgoAAAANSUhEUgAAAG0AAABmCAYAAADBPx+VAAAACXBIWXMAAAsTAAALEwEAmpwYAAAA..."]
    }
  ]
}'
```

- Location: **`messages[].images`**, a top-level array on the message object.
  There are **no** OpenAI-style content parts.
- **Bare base64, NO `data:image/png;base64,` prefix.** Proof: `type ImageData []byte`
  (types.go:57) and `Images []ImageData` — Go's `encoding/json` emits `[]byte` as
  plain standard-base64, and the doc example string starts directly with `iVBORw0KGgo`
  (a raw PNG magic in base64).
- In Go you can just do `Images: [][]byte{pngBytes}` and let `encoding/json` encode it.

### 1.4 Embeddings — `/api/embed` is current, `/api/embeddings` is deprecated

api.md L1809 verbatim: *"> Note: this endpoint has been superseded by `/api/embed`"*

**Current — `POST /api/embed`** (api.md L1689):
- `model` — name of model
- `input` — "text or list of text to generate embeddings for" ⇒ **string OR array of strings; batching supported**
- `truncate` (bool, default `true`), `options`, `keep_alive` (default `5m`), `dimensions` (int)

Go type (types.go:602): `Input any \`json:"input"\``, `Dimensions int \`json:"dimensions,omitempty"\``

Response field holding vectors: **`embeddings`**, type `[][]float32`
(types.go:623 `Embeddings [][]float32 \`json:"embeddings"\``).

```shell
curl http://localhost:11434/api/embed -d '{
  "model": "all-minilm",
  "input": ["Why is the sky blue?", "Why is the grass green?"]
}'
```
```json
{
  "model": "all-minilm",
  "embeddings": [
    [0.010071029, -0.0017594862, 0.05007221],
    [-0.0098027075, 0.06042469, 0.025257962]
  ],
  "total_duration": 14143917,
  "load_duration": 1019500,
  "prompt_eval_count": 8
}
```

**Deprecated — `POST /api/embeddings`**: takes `prompt` (single string, NOT `input`),
returns singular **`embedding`** as a flat `[]float64`:
```json
{ "embedding": [0.5670403838157654, 0.009260174818336964, 0.23178744316101074] }
```

Use `/api/embed` + `embeddings [][]float32`.

### 1.5 `options` object — exact names (modelfile.mdx "Valid Parameters and Values" table)

Your guesses are all **correct**:

| field | type | default | source |
|---|---|---|---|
| `num_ctx` | int | 2048 | modelfile.mdx L153 |
| `temperature` | float | 0.8 | L156 |
| `top_p` | float | 0.9 | L162 |
| `repeat_penalty` | float | 1.1 | L155 |
| `seed` | int | 0 | L157 |

Also present: `repeat_last_n` (int, 64), `num_predict` (int, -1), `top_k` (int, 40),
`min_p` (float, 0.0), `stop` (string, repeatable), `draft_num_predict` (int).

```json
"options": { "num_ctx": 4096, "temperature": 0.2, "top_p": 0.9, "repeat_penalty": 1.1, "seed": 42 }
```

### 1.6 `keep_alive` — 6GB GPU model swapping

Goes at the **top level of the request body** (sibling of `model`/`messages`), on
`/api/generate`, `/api/chat`, and `/api/embed`.

Accepted formats (faq.mdx L297-302, verbatim):
- a duration string (such as `"10m"` or `"24h"`)
- a number in seconds (such as `3600`)
- any negative number which will keep the model loaded in memory (e.g. `-1` or `"-1m"`)
- **`'0'` which will unload the model immediately after generating a response**

Corroborated by `Duration.UnmarshalJSON` (types.go:1243): `float64` → `t * time.Second`,
negative → `MaxInt64`; `string` → `time.ParseDuration`; default when absent → 5 min.

**Unload immediately with an empty messages array** (api.md L1142, verbatim:
"If the messages array is empty and the `keep_alive` parameter is set to `0`, a model
will be unloaded from memory"):

```shell
curl http://localhost:11434/api/chat -d '{
  "model": "llama3.2",
  "messages": [],
  "keep_alive": 0
}'
```
```json
{
  "model": "llama3.2",
  "created_at": "2024-09-12T21:33:17.547535Z",
  "message": { "role": "assistant", "content": "" },
  "done_reason": "unload",
  "done": true
}
```

Preload without generating (api.md L1114: "If the messages array is empty, the model
will be loaded into memory") — `done_reason: "load"`:
```shell
curl http://localhost:11434/api/chat -d '{"model":"llama3.2","messages":[]}'
```

Alternating two models on 6GB: send `"keep_alive": 0` on the **last** request to model A
(it unloads right after responding), then hit model B. Or explicitly unload A with the
empty-messages + `keep_alive: 0` call above before loading B. Check what's resident with
`GET /api/ps` → `models[].name`, `models[].size_vram`, `models[].expires_at` (api.md L1764).

### 1.7 Is a model pulled?

**`GET /api/tags`** — "List models that are available locally." (api.md L1381).
Returns `{"models":[{"name":"llama3.2:latest","model":"llama3.2:latest","modified_at":...,"size":...,"digest":...,"details":{...}}]}`.
Match on `models[].model` (tag-qualified; `latest` is implied when omitted, api.md L25).

**`POST /api/show`** (api.md L1438) — `{"model": "llava", "verbose": false}`.
Response keys: `modelfile`, `parameters`, `template`, `details{parent_model, format,
family, families, parameter_size, quantization_level}`, `model_info{}`,
`capabilities[]` (e.g. `["completion","vision"]`).
`capabilities` is the right field to test for **vision** support before sending images.

Prefer `/api/tags` for a plain "is it pulled" check (documented semantics);
`/api/show` for capability probing. The HTTP status returned by `/api/show` for a
missing model is not documented — see UNVERIFIED.

---

## 2. `github.com/ledongthuc/pdf`

Primary: `https://github.com/ledongthuc/pdf` — README, `read.go`, `page.go`.

### Version for go.mod

No semver tags exist. Latest pseudo-version from the Go module proxy
(`https://proxy.golang.org/github.com/ledongthuc/pdf/@latest`):

```
github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
```
Its `go.mod` declares `go 1.24.1`. Zero dependencies. License: BSD-3-Clause
(the Go Authors license — it's a fork of `rsc/pdf`).

### Exact API (read.go / page.go)

```go
func Open(file string) (*os.File, *Reader, error)                    // read.go:105
func NewReader(f io.ReaderAt, size int64) (*Reader, error)           // read.go:125
func NewReaderEncrypted(f io.ReaderAt, size int64, pw func() string) (*Reader, error) // read.go:133

func (r *Reader) NumPage() int                                       // page.go:59
func (r *Reader) Page(num int) Page                                  // page.go:25  — 1-BASED
func (r *Reader) GetPlainText() (reader io.Reader, err error)        // page.go:64
func (r *Reader) GetStyledTexts() (sentences []Text, err error)      // page.go:86

func (p Page) GetPlainText(fonts map[string]*Font) (result string, err error)
func (p Page) GetTextByRow() (Rows, error)                           // page.go:690
func (p Page) Content() Content
func (p Page) Fonts() []string
func (p Page) Font(name string) Font
func (p Page) MediaBox() Value
func (p Page) CropBox() Value
```

```go
type Text struct {          // page.go:478
	Font     string  // the font used
	FontSize float64 // points
	X        float64 // increasing left to right
	Y        float64 // increasing bottom to top
	W        float64 // width of the text, in points
	S        string  // the actual UTF-8 text
}

type Row struct {           // page.go:681
	Position int64           // int64(Y)
	Content  TextHorizontal  // []Text
}
type Rows []*Row
type TextHorizontal []Text  // sort.Interface: X asc, then Y desc
```

`Page.V` is a `Value`; guard with `p.V.IsNull() || p.V.Key("Contents").Kind() == pdf.Null`.

### Minimal working example (whole-doc plain text)

```go
package main

import (
	"bytes"
	"fmt"

	"github.com/ledongthuc/pdf"
)

func main() {
	f, r, err := pdf.Open("./doc.pdf")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	b, err := r.GetPlainText() // io.Reader
	if err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(b)
	fmt.Println(buf.String())
}
```

### Per-page, reading-order example

```go
func readPDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var out bytes.Buffer
	for i := 1; i <= r.NumPage(); i++ { // 1-based
		p := r.Page(i)
		if p.V.IsNull() || p.V.Key("Contents").Kind() == pdf.Null {
			continue
		}
		rows, err := p.GetTextByRow()
		if err != nil {
			return "", err
		}
		for _, row := range rows { // rows sorted top->bottom
			for _, w := range row.Content { // words sorted left->right
				out.WriteString(w.S)
			}
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}
```

### Two extraction paths — which preserves reading order

**Yes, there are two, and they differ materially.**

1. `Reader.GetPlainText() (io.Reader, error)` → loops pages 1..N calling
   `Page.GetPlainText(fonts)`. That method **walks the content stream and appends
   text in raw operator order** (`Interpret(strm, ...)`; it only writes `"\n"` on the
   `BT` operator). **No coordinate sorting at all.** Fast, but multi-column /
   table-heavy / oddly-ordered PDFs come out scrambled.
2. `Page.GetTextByRow() (Rows, error)` → buckets runs by Y, then
   `sort.Sort(row.Content)` (`TextHorizontal`: X ascending) and
   `sort.Slice(result, ...)` with `result[i].Position > result[j].Position`
   (Y descending = top of page first) — page.go:743-749.

⇒ **`GetTextByRow()` preserves reading order much better.** Use it. (There is also
`GetStyledTexts()`, which merges same-style runs into sentences via `IsSameSentence`
but does **not** sort — it walks `p.Content().Text` in stream order.)

Caveats read from source:
- `Page.GetPlainText` and `Page.GetTextByRow` both `defer recover()` and convert panics
  into `error`. `Reader.GetStyledTexts` has **no** recover — a malformed PDF panics.
- `GetTextByRow` inserts no inter-word spaces; concatenating `word.S` can glue words.
  Add spacing yourself using `Text.X` / `Text.W` if it matters.
- No password prompt in `Open`; use `NewReaderEncrypted` for encrypted files.

---

## 3. PDF page → PNG rasterization from Go on Windows

### 3a. Pure-Go (no cgo) rasterizer — does one exist?

**Not a real pure-Go one.** No library implements a PDF graphics-model rasterizer in
Go alone. `pdfcpu`, `ledongthuc/pdf`, `rsc.io/pdf`, `unidoc` (community builds) do not
rasterize.

The practical **no-cgo** answer is **`github.com/klippa-app/go-pdfium` in WebAssembly
mode**: PDFium compiled to WASM, embedded via `go:embed`, executed by the pure-Go
Wazero runtime. It is genuinely `CGO_ENABLED=0` and cross-compiles to Windows.

README verbatim (`https://raw.githubusercontent.com/klippa-app/go-pdfium/main/README.md` L88):
*"`webassembly` does not need any external dependencies and also does not require CGO to work."*
And L435: *"It's about 2x as slow as the full native cgo implementation (but about 2x as fast as the multi-threaded cgo go-plugin [implementation])."*

- Latest: `github.com/klippa-app/go-pdfium v1.19.6` (proxy.golang.org, 2026-07-27); `go.mod` says `go 1.23.0`, `toolchain go1.24.1`.
- Licenses: **go-pdfium = MIT** (LICENSE file, "Copyright (c) 2022 Klippa App BV");
  **PDFium = Apache License 2.0** (README L71); **Wazero = Apache License 2.0** (README L442).
  All permissive — safe for a closed-source production server.
- Maturity: full PDFium public API coverage, CI + codecov, actively tracks PDFium releases.

Working render call (README L557-635, verbatim structure):

```go
import (
	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

var pool pdfium.Pool
var instance pdfium.Pdfium

func init() {
	var err error
	pool, err = webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
	if err != nil { log.Fatal(err) }
	instance, err = pool.GetInstance(time.Second * 30)
	if err != nil { log.Fatal(err) }
}

func renderPage(filePath string, pageIndex int, output string) error {
	pdfBytes, err := os.ReadFile(filePath)
	if err != nil { return err }

	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &pdfBytes})
	if err != nil { return err }
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	pageRender, err := instance.RenderPageInDPI(&requests.RenderPageInDPI{
		DPI: 200,
		Page: requests.Page{
			ByIndex: &requests.PageByIndex{Document: doc.Document, Index: pageIndex}, // 0-indexed
		},
	})
	if err != nil { return err }
	defer pageRender.Cleanup() // required in webassembly mode

	f, err := os.Create(output)
	if err != nil { return err }
	defer f.Close()
	return png.Encode(f, pageRender.Result.Image) // image.Image
}
```

Windows note from README L448/L453: in WASM mode go-pdfium mounts the root disk on
non-Windows; **all paths passed in WASM mode must be POSIX-style and absolute**, so on
Windows prefer the `File: &pdfBytes` (in-memory) form shown above rather than a path.

### 3b. `github.com/gen2brain/go-fitz`

Source: `https://github.com/gen2brain/go-fitz` (README, COPYING, repo tree).

- Latest **v1.28.2** (2026-07-06). `go.mod`: `go 1.24.0`; deps `github.com/ebitengine/purego v0.10.1`, `golang.org/x/sys v0.33.0`.
- **cgo: yes by default.** README build tags list `nocgo` as *"experimental purego implementation (can also be used with `CGO_ENABLED=0`)"* — so cgo-free is possible but explicitly experimental, and in that mode it `syscall.LoadLibrary("libmupdf.dll")` at runtime (`purego_windows.go`, `const libname = "libmupdf.dll"`) — **you must supply libmupdf.dll yourself; the repo does not ship one.**
- **Windows: yes** for the default cgo path — the repo bundles prebuilt static libs
  `libs/libmupdf_windows_amd64.a`, `libs/libmupdfthird_windows_amd64.a` (+ `_arm64`),
  and has `purego_windows.go` / `stderr_windows.go` / `struct_windows.go`.
  Practical cost: cgo on Windows needs a MinGW-w64 gcc on PATH and `CGO_ENABLED=1`;
  linking the two ~100MB static archives makes builds slow and binaries large.
- **License: AGPL-3.0.** The repo's `COPYING` is verbatim "GNU AFFERO GENERAL PUBLIC
  LICENSE Version 3, 19 November 2007". MuPDF itself is AGPL-3.0-or-later from Artifex,
  with a paid commercial license as the alternative. **AGPL §13 reaches network users** —
  a hosted Go server linking MuPDF must offer its complete corresponding source to users,
  or you buy an Artifex commercial license.

Public API (`fitz_cgo.go`):

```go
func New(filename string) (*Document, error)
func NewFromMemory(b []byte) (*Document, error)
func NewFromReader(r io.Reader) (*Document, error)

func (f *Document) NumPage() int
func (f *Document) Image(pageNumber int) (*image.RGBA, error)
func (f *Document) ImageDPI(pageNumber int, dpi float64) (*image.RGBA, error)
func (f *Document) ImagePNG(pageNumber int, dpi float64) ([]byte, error)   // ← the one you want
func (f *Document) Text(pageNumber int) (string, error)
func (f *Document) HTML(pageNumber int, header bool) (string, error)
func (f *Document) SVG(pageNumber int) (string, error)
func (f *Document) Bound(pageNumber int) (image.Rectangle, error)
func (f *Document) Links(pageNumber int) ([]Link, error)
func (f *Document) ToC() ([]Outline, error)
func (f *Document) Metadata() map[string]string
func (f *Document) Close() error
```
Page numbers are **0-indexed** (README loops `for n := 0; n < doc.NumPage(); n++`).
README also warns: concurrency is per-`Document` only — *"Concurrency on a single
`Document`, including racing with `Close`, is not supported."*

### 3c. Shelling out to poppler (`pdftoppm` / `pdftocairo`) on Windows

Install — verified package manifests:

| manager | command | evidence |
|---|---|---|
| **scoop** | `scoop install poppler` | `https://raw.githubusercontent.com/ScoopInstaller/Main/master/bucket/poppler.json` — version `26.02.0-0`, pulls `oschwartz10612/poppler-windows` release zip, shims `bin\pdftoppm.exe`, `bin\pdftocairo.exe`, etc. |
| **winget** | `winget install oschwartz10612.Poppler` | manifest dir exists at `microsoft/winget-pkgs/manifests/o/oschwartz10612/Poppler` (versions incl. `24.08.0-0`, `25.07.0-0`) |
| **manual** | download `Release-<ver>.zip` from `https://github.com/oschwartz10612/poppler-windows/releases`, add `<extract>\Library\bin` to PATH | that repo is the source both managers use |

Chocolatey: not confirmed — see UNVERIFIED.
Poppler itself is **GPL-2.0-or-later**, so shell out (separate process, no linking); do not statically bundle into a proprietary binary.

Exact command line — render **page N** to PNG at a given DPI
(flags verbatim from the poppler man page
`https://gitlab.freedesktop.org/poppler/poppler/-/raw/master/utils/pdftoppm.1`:
`-f number` first page, `-l number` last page, `-r number` X and Y resolution in DPI
(default 150), `-png` "Generates a PNG file instead a PPM file",
`-singlefile` "Writes only the first page and does not add digits"):

```powershell
pdftoppm -png -r 300 -f 7 -l 7 -singlefile "input.pdf" "out\page7"
# -> out\page7.png     (with -singlefile, no -NN suffix is appended)
```

Without `-singlefile` the output is `<root>-<number>.png` (man page: "writes one PPM
file for each page, PPM-root-number.ppm").

`pdftocairo` is the anti-aliased Cairo-backed sibling from the same package and takes
`-png -r -f -l -singlefile` identically.

Go invocation:
```go
n := 7
cmd := exec.Command("pdftoppm", "-png", "-r", "300",
	"-f", strconv.Itoa(n), "-l", strconv.Itoa(n), "-singlefile",
	inPath, filepath.Join(outDir, fmt.Sprintf("page%d", n)))
out, err := cmd.CombinedOutput()
```

### 3d. Ghostscript

Install on Windows: **`scoop install ghostscript`** — verified manifest
`https://raw.githubusercontent.com/ScoopInstaller/Main/master/bucket/ghostscript.json`
(version 10.07.1, downloads `gs10071w64.exe` from
`ArtifexSoftware/ghostpdl-downloads` releases, shims `bin\gswin64c.exe` and alias `gs`).
Manifest license field: **`AGPL-3.0-or-later|Freeware`**. No winget package exists under
`ArtifexSoftware` (that publisher folder only contains `mutool`).

Exact command line (options verbatim from
`https://ghostscript.readthedocs.io/en/latest/Use.html`: `-o` "also sets the `-dBATCH`
and `-dNOPAUSE` options"; `-sDEVICE` selects `png16m`/`pngalpha`/`pnggray`; `-r` sets DPI;
`-dFirstPage=` / `-dLastPage=` bound the page range):

```powershell
gswin64c -q -dSAFER -sDEVICE=png16m -r300 -dFirstPage=7 -dLastPage=7 -o "out\page7.png" "input.pdf"
```
(the executable is `gswin64c.exe` on Windows — the console build; `gs` is the POSIX name
and the scoop alias.)

Bonus, also verified: **`mutool`** has a winget package
(`microsoft/winget-pkgs/manifests/a/ArtifexSoftware/mutool`, versions 1.22.1 / 1.23.0):
```powershell
winget install ArtifexSoftware.mutool
mutool draw -r 300 -o page%d.png input.pdf 7
```
(`mutool draw [options] file [pages]`; `-r resolution` default 72dpi; `-o` infers format
from the filename, `%d` = page number; pages are a 1-based comma-separated range —
`https://mupdf.readthedocs.io/en/latest/tools/mutool-draw.html`). Same AGPL as MuPDF,
but as a subprocess it's mere aggregation, not linking.

### 3e. Recommendation

**Throwaway prototype on Windows:** shell out to **`pdftoppm`** via `os/exec`.
`scoop install poppler`, one `exec.Command`, no cgo, no C toolchain, no build-tag games,
5 minutes to working. Ghostscript is the fallback if poppler chokes on a file.

**Production Go server:** **`github.com/klippa-app/go-pdfium` in `webassembly` mode.**
MIT + Apache-2.0 + Apache-2.0 all the way down, `CGO_ENABLED=0`, single self-contained
binary, cross-compiles, sandboxed (a malformed PDF can't segfault your process), and
no external binary to provision in the container/host. Cost: ~2x slower than native cgo
PDFium, and WASM memory is never returned to the OS (README L437) — so cap
`MaxTotal` workers and recycle instances on a long-running server.

**Avoid `go-fitz` for a closed-source hosted service**: it is AGPL-3.0 and MuPDF is
AGPL-3.0-or-later; §13 triggers on network use. It's fine for internal tooling, GPL-compatible
projects, or if you buy Artifex's commercial license. Also it wants a MinGW gcc on Windows.

---

## 4. `github.com/expr-lang/expr`

Primary: `https://github.com/expr-lang/expr` — `expr.go`, `conf/config.go`, `conf/env.go`,
`compiler/compiler.go`, `LICENSE`.

- **Latest: `v1.17.8`** (proxy.golang.org, 2026-02-14). `go.mod` declares `go 1.18`.
- **License: MIT** ("MIT License / Copyright (c) 2018 Anton Medvedev").
- Install: `go get github.com/expr-lang/expr`

### Signatures (expr.go)

```go
func Compile(input string, ops ...Option) (*vm.Program, error)  // expr.go:229
func Run(program *vm.Program, env any) (any, error)             // expr.go:264
func Eval(input string, env any) (any, error)                   // expr.go:269
```

Relevant `Option`s (all in expr.go):
```go
func Env(env any) Option              // :29
func AllowUndefinedVariables() Option // :37
func AsFloat64() Option               // :105
func AsInt() / AsInt64() / AsBool() / AsAny() / AsKind(reflect.Kind) Option
func DisableAllBuiltins() Option      // :174
func DisableBuiltin(name string) Option // :183
func EnableBuiltin(name string) Option  // :190
func MaxNodes(n uint) Option          // :222  (default conf.DefaultMaxNodes = 1e4; 0 disables the check)
func Optimize(b bool) Option          // :131  (default true)
func Function(name string, fn func(params ...any) (any, error), types ...any) Option
func Timezone(name string) Option     // :209  — panics on bad tz name
```

**`AsFloat64()` really coerces**: `compiler/compiler.go:54` emits `OpCast, 2` for
`reflect.Float64`, so `Run` returns a `float64` even if the expression evaluates to an int.
`Run` still returns `any` — you must type-assert.

Error handling: `Compile` returns a `*file.Error` (rich, with source position and a caret
line); `Run` returns a plain error. Both are just `error` at the call site.

### Complete minimal example — `(1200*0.07)/12` → float64

```go
package main

import (
	"fmt"
	"log"

	"github.com/expr-lang/expr"
)

func main() {
	const src = `(1200*0.07)/12`

	program, err := expr.Compile(src,
		expr.Env(map[string]any{}), // strict: any identifier is a compile error
		expr.AsFloat64(),           // force a float64 result
		expr.DisableAllBuiltins(),  // no all/any/len/now/... available
		expr.MaxNodes(100),         // cheap DoS guard
	)
	if err != nil {
		log.Fatalf("compile: %v", err) // *file.Error, prints source + caret
	}

	out, err := expr.Run(program, map[string]any{})
	if err != nil {
		log.Fatalf("run: %v", err)
	}

	v, ok := out.(float64)
	if !ok {
		log.Fatalf("unexpected result type %T", out)
	}
	fmt.Println(v) // 7
}
```
Compile once, `Run` many times — `*vm.Program` is reusable.

### Sandboxing to arithmetic only

Yes, and it's the library's stated design (README "Safety and Isolation": Memory-Safe,
Side-Effect-Free, Always Terminating).

- **No function calls**: `expr.DisableAllBuiltins()` (expr.go:174) marks every entry in
  `c.Builtins` disabled; `Compile` then `delete`s them from the builtin map before
  type-checking (expr.go:234-236). Individually: `expr.DisableBuiltin("now")`.
- **No variables / no host access**: pass `expr.Env(map[string]any{})` and do **not** pass
  `AllowUndefinedVariables()`. `conf/env.go:EnvWithCache` sets `n.Strict = true` for both
  the `reflect.Map` and `reflect.Struct` branches, so any unknown identifier fails at
  compile time with "unknown name x". (An empty `struct{}{}` env works identically.)
- **Bounded program size**: `expr.MaxNodes(n)`.
- Expr has no loops and no goto by design — "Always Terminating".

Residual surface after the above: arithmetic/comparison/logical operators, string and
slice literals, indexing, and the pipe/ternary syntax. There is no I/O, no reflection
into arbitrary Go values you didn't put in `env`, and no way to reach the host process.

---

## 5. Go 1.25.0 compatibility

Your `go 1.25.0` satisfies every one of these; nothing requires a newer toolchain.

| module | version | `go.mod` directive | verdict |
|---|---|---|---|
| `github.com/ledongthuc/pdf` | `v0.0.0-20250511090121-5959a4027728` | `go 1.24.1` | ✅ pure Go, no Windows issues |
| `github.com/expr-lang/expr` | `v1.17.8` | `go 1.18` | ✅ pure Go, no Windows issues |
| `github.com/klippa-app/go-pdfium` | `v1.19.6` | `go 1.23.0` / `toolchain go1.24.1` | ✅ WASM mode is `CGO_ENABLED=0`; on Windows pass PDFs as bytes, not paths (README L448/L453) |
| `github.com/gen2brain/go-fitz` | `v1.28.2` | `go 1.24.0` | ⚠️ Windows: default path needs cgo + MinGW-w64 gcc; `nocgo` path is experimental and needs `libmupdf.dll` at runtime; AGPL-3.0 |

go.mod lines:

```
require (
	github.com/expr-lang/expr v1.17.8
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
	github.com/klippa-app/go-pdfium v1.19.6
)
```

Windows gotchas worth flagging beyond the table:
- `pdftoppm` / `gswin64c` / `mutool` are only on PATH after a scoop/winget shim — an
  `exec.LookPath` preflight with a clear "run `scoop install poppler`" error saves support time.
- `exec.Command` on Windows: pass args as separate strings (as above); do not pre-quote.

---

## UNVERIFIED / could not confirm

1. **Chocolatey poppler / ghostscript package IDs.** I verified scoop (both) and winget
   (`oschwartz10612.Poppler`, `ArtifexSoftware.mutool`) against real manifests. I did not
   reach community.chocolatey.org, so I cannot state a choco package name.
2. **No winget Ghostscript package.** I checked `manifests/a/ArtifexSoftware` (only
   `mutool`) and grepped `manifests/g` for "ghost" (only unrelated publishers). It may exist
   under a publisher folder I did not enumerate — treat "no winget Ghostscript" as
   probable-but-not-exhaustively-proven. `scoop install ghostscript` IS verified.
3. **HTTP status `/api/show` returns for a model that is not pulled.** Not stated in
   api.md. Use `GET /api/tags` for the pulled-or-not check.
4. **Whether Ollama `format` as a JSON Schema supports the full JSON Schema spec**
   (`$ref`, `oneOf`, `pattern`, etc.). The doc only demonstrates flat
   `type`/`properties`/`required`. Assume a constrained subset; test your schema.
5. **`dimensions` support on `/api/embed` is model-dependent** — documented as a
   parameter, but which models honor it is not stated.
6. **Where to obtain a Windows `libmupdf.dll`** for go-fitz's `nocgo`/purego mode.
   The repo ships only static `.a` archives; no DLL and no download instructions.
7. **go-fitz on Windows: exact toolchain requirement.** The bundled `.a` files imply
   MinGW-w64 gcc, but the README states no Windows build prerequisites, and I did not
   read the CI workflow. Treat "needs MinGW-w64" as strong inference, not documented fact.
8. **go-pdfium WASM cold-start latency and per-worker memory footprint on Windows.**
   README gives no numbers; the ~18MB embedded-binary figure came from a third-party
   summary, not the repo.
9. **`Reader.GetPlainText` behavior on encrypted PDFs opened via plain `Open`.** Not
   documented; `NewReaderEncrypted` exists but the failure mode of `Open` on an encrypted
   file was not traced.
