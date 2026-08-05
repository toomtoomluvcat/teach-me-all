// Command protoexam is a THROWAWAY prototype. See README.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"protoexam/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := app.Run(ctx); err != nil {
		code := 1
		var exitErr *app.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
		}
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(code)
	}
}
