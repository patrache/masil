// Package i18n decides the UI language and serves the typed message catalog.
//
// Messages live in locales/<lang>/<area>.toml (ko is the source of keys,
// params and meta). "go generate ./internal/i18n" turns them into one typed
// field per area on T (zz_*_gen.go — never edit by hand). It also rewrites the
// TOML files sorted and formatted: the comment block at the top of a file and
// comment lines right above a [key] (the key's translator note) are kept; a
// comment anywhere else (inside a table, after a value) is a generator error,
// never dropped silently. Translator context belongs in desc. Screens read ctx.T;
// code without a ui.Context uses For(locale) at call time, never in package
// variables or init.
package i18n

//go:generate go run ./gen
