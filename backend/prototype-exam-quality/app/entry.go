package app

import (
	"context"
	"flag"
)

// ExitError carries the process exit code for errors at the CLI boundary.
// Keeping this here lets cmd/protoexam remain a genuinely thin entrypoint
// while preserving the prototype's distinction between invalid flags and a
// failed extraction/model run.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// Run loads the CLI configuration and starts the throwaway application.
func Run(ctx context.Context) error {
	_ = loadDotEnv()
	cfg := parseConfig()
	if err := applyConfigDefaults(&cfg); err != nil {
		if cfg.pdfPath == "" {
			flag.Usage()
		}
		return &ExitError{Code: 2, Err: err}
	}
	return run(ctx, cfg)
}
