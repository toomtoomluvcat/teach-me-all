package pdfx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// findTool locates a poppler binary.
//
// It searches real poppler installs before falling back to PATH, and that order
// matters: Git for Windows ships an Xpdf 4.00 binary also called pdftotext, it
// is on PATH ahead of anything else, and it drops Thai characters that poppler
// 25 reads correctly. Taking whatever PATH offers first silently picks the worse
// tool.
func findTool(name string) (string, error) {
	exe := name
	if os.PathSeparator == '\\' {
		exe = name + ".exe"
	}

	for _, dir := range popplerDirs() {
		p := filepath.Join(dir, exe)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}

	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	return "", fmt.Errorf(
		"%s not found. install poppler:\n"+
			"  winget install oschwartz10612.Poppler\n"+
			"  or scoop install poppler\n"+
			"  or https://github.com/oschwartz10612/poppler-windows/releases", name)
}

// popplerDirs returns candidate bin directories, newest version first. The
// winget layout nests the version in a directory name, so it is globbed rather
// than hardcoded.
func popplerDirs() []string {
	var out []string

	add := func(pattern string) {
		matches, _ := filepath.Glob(pattern)
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		out = append(out, matches...)
	}

	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		add(filepath.Join(local, "Microsoft", "WinGet", "Packages", "oschwartz10612.Poppler*", "poppler-*", "Library", "bin"))
		add(filepath.Join(local, "Microsoft", "WinGet", "Packages", "oschwartz10612.Poppler*", "Library", "bin"))
	}
	if home := os.Getenv("USERPROFILE"); home != "" {
		add(filepath.Join(home, "scoop", "apps", "poppler", "current", "bin"))
	}
	add(filepath.Join("C:", "Program Files", "poppler*", "Library", "bin"))

	return out
}
