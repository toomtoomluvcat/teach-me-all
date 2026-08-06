package app

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"protoexam/examgen"
	"protoexam/pdfx"
)

// config is the CLI boundary. Keep provider and pipeline options here so the
// application runner can consume one validated value instead of reading flags
// throughout the execution path.
type config struct {
	provider           string
	pdfPath            string
	extract            string
	model              string
	embedModel         string
	extractDir         string
	doclingPython      string
	doclingOCREngine   string
	doclingOCRLang     string
	doclingOCRMode     string
	doclingFormulaMode string
	doclingOCRFullPage bool
	host               string
	geminiHost         string
	geminiAPIKey       string
	baseURL            string
	apiKey             string
	geminiMinInterval  time.Duration
	forceCalc          bool
	benchmark          string
	benchmarkLesson    string
	pages              string
	budget             int
	setCandidates      int
	contractPreflight  bool
	stopOnFullSet      bool
	fresh              bool
	extractOnly        bool
	calcTool           bool
	filterTopics       bool
	parallel           int
}

type extractionCache struct {
	Pages    []examgen.Page       `json:"pages"`
	Mode     string               `json:"mode"`
	Prepared *pdfx.PreparedBundle `json:"prepared,omitempty"`
}

// parseConfig owns flag registration. Defaults that depend on the selected
// provider are applied separately after flag.Parse so this function remains a
// straightforward description of the CLI surface.
func parseConfig() config {
	var cfg config
	flag.StringVar(&cfg.provider, "provider", "ollama", "model provider: ollama | openai | gemini, or a preset name such as deepseek")
	flag.StringVar(&cfg.pdfPath, "pdf", "", "path to the source PDF (required)")
	flag.StringVar(&cfg.extract, "extract", "auto", "extraction mode: auto | docling (both use Docling; auto is the default name)")
	flag.StringVar(&cfg.extractDir, "extract-dir", "", "directory for the extraction bundle; default .scratch/<hash>/extract")
	flag.StringVar(&cfg.doclingPython, "docling-python", "", "Python with Docling installed; auto-detected from .scratch/docling-venv")
	flag.StringVar(&cfg.doclingOCREngine, "docling-ocr-engine", "auto", "Docling OCR engine: auto | rapidocr | easyocr")
	flag.StringVar(&cfg.doclingOCRLang, "docling-ocr-lang", "th,en", "Docling OCR languages; defaults to Thai + English")
	flag.StringVar(&cfg.doclingOCRMode, "docling-ocr", "auto", "Docling OCR mode: auto | on | off; auto detects native PDF text")
	flag.StringVar(&cfg.doclingFormulaMode, "docling-formulas", "auto", "Docling formula enrichment: auto | on | off")
	flag.BoolVar(&cfg.doclingOCRFullPage, "docling-ocr-full-page", false, "force OCR over complete pages instead of only detected regions")
	flag.StringVar(&cfg.model, "model", "", "generation and judge model (provider default when empty)")
	// bge-m3, not nomic-embed-text. Measured on Thai question pairs,
	// nomic returns cosine 1.0000 for every pair in the same chapter whether
	// they are the same question or not — no threshold separates them, and
	// chunk ranking built on it is random. bge-m3 scores duplicates at 0.95+
	// and different questions at 0.60, a usable gap of 0.31.
	flag.StringVar(&cfg.embedModel, "embed-model", "", "embedding model, empty to use the provider default")
	flag.StringVar(&cfg.host, "host", "http://localhost:11434", "Ollama host")
	flag.StringVar(&cfg.geminiHost, "gemini-host", "https://generativelanguage.googleapis.com", "Gemini API host")
	flag.StringVar(&cfg.geminiAPIKey, "gemini-api-key", "", "Gemini API key (prefer GEMINI_API_KEY)")
	flag.DurationVar(&cfg.geminiMinInterval, "gemini-min-interval", 13*time.Second, "minimum delay between Gemini requests; 0 disables throttling")
	flag.StringVar(&cfg.baseURL, "base-url", "", "OpenAI-compatible base URL, without /chat/completions; e.g. https://openrouter.ai/api/v1 or http://localhost:8000/v1")
	flag.StringVar(&cfg.apiKey, "api-key", "", "API key for the OpenAI-compatible provider (prefer LLM_API_KEY; a local server usually needs none)")
	flag.BoolVar(&cfg.forceCalc, "force-calc", false, "require arithmetic on every generated question (skill remains cognitive demand)")
	flag.StringVar(&cfg.benchmark, "benchmark", "", "run benchmark suite: all | application-easy | application-medium | application-hard | calculation")
	flag.StringVar(&cfg.benchmarkLesson, "benchmark-lesson", "", "lesson title fragment for a generic benchmark; enables subject-neutral directives")
	flag.StringVar(&cfg.pages, "pages", "", "page range, e.g. 10-40")
	flag.IntVar(&cfg.budget, "budget", 0, "override the model's own question budget")
	flag.IntVar(&cfg.setCandidates, "set-candidates", 3, "independent set candidates to generate and score when --set-generation is enabled")
	flag.BoolVar(&cfg.contractPreflight, "contract-preflight", true, "normalize/drop deterministic coverage-slot defects before generation")
	flag.BoolVar(&cfg.stopOnFullSet, "stop-on-full-set", false, "stop generating candidates once one covers every contract slot; trades set variety for calls")
	flag.BoolVar(&cfg.fresh, "fresh", false, "ignore the cache")
	flag.BoolVar(&cfg.extractOnly, "extract-only", false, "stop after extraction; needs no models and no Ollama")
	flag.BoolVar(&cfg.filterTopics, "filter-topics", true, "drop chunks pass 1 classified as teacher-guide apparatus or page furniture; set false for a source that really is mostly exercises")
	flag.BoolVar(&cfg.calcTool, "calc-tool", true, "let the model use a calculator tool before writing questions with arithmetic")
	flag.IntVar(&cfg.parallel, "parallel", 4, "model calls in flight at once; Ollama also needs OLLAMA_NUM_PARALLEL to match")
	flag.Parse()
	return cfg
}

// applyConfigDefaults resolves environment-backed credentials and defaults
// that depend on the selected provider. It also keeps CLI validation in one
// place, before any provider or PDF work begins.
func applyConfigDefaults(cfg *config) error {
	if cfg.geminiAPIKey == "" {
		cfg.geminiAPIKey = os.Getenv("GEMINI_API_KEY")
	}
	if err := applyProviderPreset(cfg); err != nil {
		return err
	}
	if cfg.apiKey == "" {
		cfg.apiKey = os.Getenv("LLM_API_KEY")
	}
	if cfg.provider != "ollama" && cfg.provider != "gemini" && cfg.provider != "openai" {
		return fmt.Errorf("--provider must be ollama, openai, or gemini (or a preset such as deepseek), got %q", cfg.provider)
	}
	if cfg.provider == "openai" && cfg.baseURL == "" {
		return fmt.Errorf("--provider openai needs --base-url, e.g. https://openrouter.ai/api/v1 or http://localhost:8000/v1")
	}
	if cfg.model == "" {
		switch cfg.provider {
		case "gemini":
			cfg.model = "gemini-2.5-flash"
		case "openai":
			return fmt.Errorf("--provider openai needs --model; the base URL does not imply one")
		default:
			cfg.model = "scb10x/typhoon2.5-qwen3-4b"
		}
	}
	// An explicitly empty --embed-model still disables ranking. Only fill in a
	// provider default when the flag was not supplied at all.
	embedModelSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "embed-model" {
			embedModelSet = true
		}
	})
	if !embedModelSet {
		switch cfg.provider {
		case "gemini":
			cfg.embedModel = "gemini-embedding-001"
		case "openai":
			// Whether the host implements /embeddings is a property of that host,
			// not of the protocol. Leave it off unless the operator names a model.
			cfg.embedModel = ""
		default:
			cfg.embedModel = "bge-m3"
		}
	}
	if cfg.pdfPath == "" {
		return fmt.Errorf("--pdf is required")
	}
	return nil
}

// applyProviderPreset expands a shorthand provider name into the generic
// OpenAI-compatible provider plus its base URL, key source, and default model.
// Everything a preset sets can still be overridden on the command line, so a
// new vendor never needs code here — only a base URL.
func applyProviderPreset(cfg *config) error {
	preset, ok := providerPresets[cfg.provider]
	if !ok {
		return nil
	}
	cfg.provider = "openai"
	if cfg.baseURL == "" {
		cfg.baseURL = preset.baseURL
	}
	if cfg.apiKey == "" {
		cfg.apiKey = os.Getenv(preset.keyEnv)
	}
	if cfg.model == "" {
		cfg.model = preset.model
	}
	return nil
}

type providerPreset struct {
	baseURL string
	keyEnv  string
	model   string
}

var providerPresets = map[string]providerPreset{
	"deepseek": {baseURL: "https://api.deepseek.com", keyEnv: "DEEPSEEK_API_KEY", model: "deepseek-chat"},
}

// loadDotEnv loads the nearest .env from the working directory or one of its
// parents. It is intentionally tiny: this prototype only needs key=value
// pairs, and an already-exported environment variable always wins.
func loadDotEnv() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		path := filepath.Join(dir, ".env")
		data, err := os.ReadFile(path)
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				line = strings.TrimPrefix(line, "export ")
				at := strings.IndexByte(line, '=')
				if at <= 0 {
					continue
				}
				key := strings.TrimSpace(line[:at])
				if os.Getenv(key) != "" {
					continue
				}
				value := strings.TrimSpace(line[at+1:])
				if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
					if unquoted, unquoteErr := strconv.Unquote(value); unquoteErr == nil {
						value = unquoted
					}
				} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
					value = value[1 : len(value)-1]
				}
				_ = os.Setenv(key, value)
			}
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}
