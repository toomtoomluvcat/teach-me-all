package app

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

func readLine(in *bufio.Scanner) (string, bool) {
	if !in.Scan() {
		return "", false
	}
	return strings.TrimSpace(in.Text()), true
}

func confirm(in *bufio.Scanner) bool {
	line, ok := readLine(in)
	return ok && line != "q"
}

func parsePages(s string) (int, int, error) {
	if strings.TrimSpace(s) == "" {
		return 0, 0, nil
	}
	parts := strings.SplitN(s, "-", 2)
	from, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("--pages %q: %w", s, err)
	}
	if len(parts) == 1 {
		return from, from, nil
	}
	to, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("--pages %q: %w", s, err)
	}
	return from, to, nil
}
