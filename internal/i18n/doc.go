// Package i18n decides the UI language and serves the typed message catalog.
//
// Messages live in locales/<lang>/<area>.toml (ko is the source of keys,
// params and meta). "go generate ./internal/i18n" turns them into one typed
// field per area on T (zz_*_gen.go — never edit by hand). Screens read ctx.T;
// code without a ui.Context uses For(locale) at call time, never in package
// variables or init.
package i18n

//go:generate go run ./gen
