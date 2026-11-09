// Package pkglevel is TestNoPackageLevelCatalogDetects' input: four
// package-level catalog lookups (a var initializer, init, and a func literal
// called on the spot in each) and two allowed ones (a function body and a func
// literal held by a package variable).
package pkglevel

import "github.com/patrache/masil/internal/i18n"

var x = i18n.For(i18n.Ko).Common.LangNameKo()

func init() { i18n.KoT() }

func f() { i18n.KoT() }

var label = func(t *i18n.T) string { return i18n.KoT().Common.LangNameEn() }

var y = func() string { return i18n.KoT().Common.LangNameKo() }()

func init() { func() { _ = i18n.For(i18n.En) }() }
