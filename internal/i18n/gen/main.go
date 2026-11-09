// Command gen builds internal/i18n's typed catalog from locales/<lang>/<area>.toml.
// Run it with "go generate ./internal/i18n"; never edit zz_*_gen.go by hand.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	locales := flag.String("locales", "locales", "locales directory (ko/, en/, zh-Hans/)")
	out := flag.String("out", ".", "directory for the generated zz_*_gen.go files")
	doFmt := flag.Bool("fmt", true, "rewrite the source TOML files sorted and formatted")
	quiet := flag.Bool("q", false, "do not print missing-translation warnings")
	flag.Parse()
	if err := run(*locales, *out, *doFmt, *quiet); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(locales, out string, doFmt, quiet bool) error {
	c, errs := load(locales)
	if c != nil {
		cerrs, warns := check(c)
		errs = append(errs, cerrs...)
		if !quiet && len(warns) > 0 {
			fmt.Fprintf(os.Stderr, "i18n: %d missing translations (allowed until the release gate):\n  %s\n",
				len(warns), strings.Join(warns, "\n  "))
		}
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "i18n:", e)
		}
		return errFailed
	}
	files, err := emit(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	// Drop generated files of areas that no longer exist.
	stale, _ := filepath.Glob(filepath.Join(out, "zz_*_gen.go"))
	for _, p := range stale {
		if _, keep := files[filepath.Base(p)]; !keep {
			if err := os.Remove(p); err != nil {
				return err
			}
		}
	}
	for name, src := range files {
		if err := writeIfChanged(filepath.Join(out, name), src); err != nil {
			return err
		}
	}
	if doFmt {
		for _, a := range c.Areas {
			for lang, f := range a.files {
				path := filepath.Join(locales, lang, a.Name+".toml")
				if err := writeIfChanged(path, formatTOML(f)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func writeIfChanged(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}
