package services

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestDocumentLinks(t *testing.T) {
	assertLinks(t, `@import 'foo.css';`, []lsp.DocumentLink{
		{Range: offsetRange(8, 17), Target: "test://test/foo.css"},
	})
	assertLinks(t, `@import './foo.css';`, []lsp.DocumentLink{
		{Range: offsetRange(8, 19), Target: "test://test/foo.css"},
	})
	assertLinks(t, `@import url("foo.css") print;`, []lsp.DocumentLink{
		{Range: offsetRange(12, 21), Target: "test://test/foo.css"},
	})
	assertLinks(t, `@import url("chrome://downloads")`, []lsp.DocumentLink{
		{Range: offsetRange(12, 32), Target: "chrome://downloads"},
	})
	assertLinks(t, `body { background-image: url(./foo.jpg)`, []lsp.DocumentLink{
		{Range: offsetRange(29, 38), Target: "test://test/foo.jpg"},
	})
	assertLinks(t, `body { background-image: url()`, []lsp.DocumentLink{})
	assertLinks(t, `body { background-image: url(data:image/gif;base64,AAAA) }`, []lsp.DocumentLink{})
}

func TestCSSNavigationLinksPortedNamedCases(t *testing.T) {
	t.Run("basic @import links", func(t *testing.T) {
		assertLinks(t, `@import 'foo.css';`, []lsp.DocumentLink{
			{Range: offsetRange(8, 17), Target: "test://test/foo.css"},
		})
		assertLinks(t, `@import './foo.css';`, []lsp.DocumentLink{
			{Range: offsetRange(8, 19), Target: "test://test/foo.css"},
		})
		assertLinks(t, `@import '../foo.css';`, []lsp.DocumentLink{
			{Range: offsetRange(8, 20), Target: "test://foo.css"},
		})
	})
	t.Run("complex @import links", func(t *testing.T) {
		assertLinks(t, `@import url("foo.css") print;`, []lsp.DocumentLink{
			{Range: offsetRange(12, 21), Target: "test://test/foo.css"},
		})
		assertLinks(t, `@import url("chrome://downloads")`, []lsp.DocumentLink{
			{Range: offsetRange(12, 32), Target: "chrome://downloads"},
		})
		assertLinks(t, `@import url('landscape.css') screen and (orientation:landscape);`, []lsp.DocumentLink{
			{Range: offsetRange(12, 27), Target: "test://test/landscape.css"},
		})
	})
	t.Run("aliased @import links", func(t *testing.T) {
		resolver := aliasLinkResolver{
			aliases: map[string]string{
				"@SingleStylesheet": "/src/assets/styles.css",
				"@AssetsDir/":       "/src/assets/",
			},
			fallback: knownLinkResolver{
				"test://test/src/assets/styles.css": true,
			},
		}
		assertLinksWithDocument(t, "test://test/test.css", "css", `@import "@SingleStylesheet"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 27), Target: "test://test/src/assets/styles.css"},
		})
		assertLinksWithDocument(t, "test://test/test.css", "css", `@import "@AssetsDir/styles.css"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 31), Target: "test://test/src/assets/styles.css"},
		})
	})
	t.Run("links in rulesets", func(t *testing.T) {
		assertLinks(t, `body { background-image: url(./foo.jpg)`, []lsp.DocumentLink{
			{Range: offsetRange(29, 38), Target: "test://test/foo.jpg"},
		})
		assertLinks(t, `body { background-image: url('./foo.jpg')`, []lsp.DocumentLink{
			{Range: offsetRange(29, 40), Target: "test://test/foo.jpg"},
		})
	})
	t.Run("No links with empty range", func(t *testing.T) {
		assertLinks(t, `body { background-image: url()`, []lsp.DocumentLink{})
		assertLinks(t, `@import url();`, []lsp.DocumentLink{})
	})
	t.Run("No links for data:", func(t *testing.T) {
		assertLinks(t, `body { background-image: url(data:image/gif;base64,R0lGODlhEAAQAMQAAORHHOVSKudfOul) }`, []lsp.DocumentLink{})
	})
	t.Run("url links", func(t *testing.T) {
		resolver := knownLinkResolver{
			"test://test/linksTestFixtures/hello.html":  true,
			"test://test/linksTestFixtures/a.css":       true,
			"test://test/linksTestFixtures/green/c.css": true,
		}
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.css", "css", `html { background-image: url("hello.html")`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(29, 41), Target: "test://test/linksTestFixtures/hello.html"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.css", "css", `@import "a.css"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 15), Target: "test://test/linksTestFixtures/a.css"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.css", "css", `@import "green/c.css"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 21), Target: "test://test/linksTestFixtures/green/c.css"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.css", "css", `@import "./green/c.css"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 23), Target: "test://test/linksTestFixtures/green/c.css"},
		})
	})
	t.Run("node module resolving", func(t *testing.T) {
		resolver := cssModuleLinkResolver{root: "test://test/linksTestFixtures/", known: knownLinkResolver{
			"test://test/linksTestFixtures/node_modules/foo/hello.html": true,
			"test://test/linksTestFixtures/node_modules/green/c.css":    true,
		}}
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.css", "css", `html { background-image: url("~foo/hello.html")`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(29, 46), Target: "test://test/linksTestFixtures/node_modules/foo/hello.html"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.css", "css", `@import "~green/c.css"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 22), Target: "test://test/linksTestFixtures/node_modules/green/c.css"},
		})
	})
	t.Run("node module subfolder resolving", func(t *testing.T) {
		resolver := cssModuleLinkResolver{root: "test://test/linksTestFixtures/", known: knownLinkResolver{
			"test://test/linksTestFixtures/node_modules/foo/hello.html": true,
			"test://test/linksTestFixtures/green/c.css":                 true,
		}}
		assertLinksWithDocument(t, "test://test/linksTestFixtures/subdir/about.css", "css", `html { background-image: url("~foo/hello.html")`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(29, 46), Target: "test://test/linksTestFixtures/node_modules/foo/hello.html"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/subdir/about.css", "css", `@import "../green/c.css"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 24), Target: "test://test/linksTestFixtures/green/c.css"},
		})
	})
	t.Run("bare module specifier resolving", func(t *testing.T) {
		resolver := cssModuleLinkResolver{root: "test://test/linksTestFixtures/", known: knownLinkResolver{
			"test://test/linksTestFixtures/node_modules/foo/bar.css": true,
		}}
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.css", "css", `@import "foo/bar.css"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 21), Target: "test://test/linksTestFixtures/node_modules/foo/bar.css"},
		})
	})
}

func TestSCSSPartialDocumentLinks(t *testing.T) {
	assertLinksWithDocument(t, "test://test/noUnderscore/index.scss", "scss", `@import 'foo'`, knownLinkResolver{
		"test://test/noUnderscore/foo.scss": true,
	}, []lsp.DocumentLink{
		{Range: offsetRange(8, 13), Target: "test://test/noUnderscore/foo.scss"},
	})
	assertLinksWithDocument(t, "test://test/underscore/index.scss", "scss", `@import 'foo.scss'`, knownLinkResolver{
		"test://test/underscore/_foo.scss": true,
	}, []lsp.DocumentLink{
		{Range: offsetRange(8, 18), Target: "test://test/underscore/_foo.scss"},
	})
	assertLinksWithDocument(t, "test://test/both/index.scss", "scss", `@import 'foo'`, knownLinkResolver{
		"test://test/both/foo.scss":  true,
		"test://test/both/_foo.scss": true,
	}, []lsp.DocumentLink{
		{Range: offsetRange(8, 13), Target: "test://test/both/foo.scss"},
	})
	assertLinksWithDocument(t, "test://test/index/index.scss", "scss", `@import 'foo'`, knownLinkResolver{
		"test://test/index/foo/index.scss": true,
	}, []lsp.DocumentLink{
		{Range: offsetRange(8, 13), Target: "test://test/index/foo/index.scss"},
	})
	assertLinksWithDocument(t, "test://test/index/index.scss", "scss", `@import 'bar'`, knownLinkResolver{
		"test://test/index/bar/_index.scss": true,
	}, []lsp.DocumentLink{
		{Range: offsetRange(8, 13), Target: "test://test/index/bar/_index.scss"},
	})
	assertLinksWithDocument(t, "test://test/missing/index.scss", "scss", `@import 'foo'`, knownLinkResolver{}, []lsp.DocumentLink{
		{Range: offsetRange(8, 13), Target: "test://test/missing/foo"},
	})
}

func TestSCSSModuleDocumentLinks(t *testing.T) {
	resolver := knownLinkResolver{"test://test/module/foo.scss": true}
	assertLinksWithDocument(t, "test://test/module/index.scss", "scss", `@use './foo' as f`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 12), Target: "test://test/module/foo.scss"},
	})
	assertLinksWithDocument(t, "test://test/module/index.scss", "scss", `@forward './foo' hide $private`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(9, 16), Target: "test://test/module/foo.scss"},
	})
	assertLinksWithDocument(t, "test://test/module/index.scss", "scss", `@use 'sass:math'`, resolver, []lsp.DocumentLink{})
}

func TestSCSSNodeModuleDocumentLinks(t *testing.T) {
	resolver := knownLinkResolver{
		"test://test/green/d.scss":                      true,
		"test://test/node_modules/foo/hello.html":       true,
		"test://test/node_modules/@foo/bar/_baz.scss":   true,
		"test://test/node_modules/@foo/bar/_index.scss": true,
		"test://test/node_modules/green/_e.scss":        true,
	}
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `html { background-image: url("~foo/hello.html")`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(29, 46), Target: "test://test/node_modules/foo/hello.html"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `html { background-image: url("foo/hello.html")`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(29, 45), Target: "test://test/node_modules/foo/hello.html"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use '@foo/bar/baz'`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 19), Target: "test://test/node_modules/@foo/bar/_baz.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use '@foo/bar'`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 15), Target: "test://test/node_modules/@foo/bar/_index.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@import "green/d"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(8, 17), Target: "test://test/green/d.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@import "green/e"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(8, 17), Target: "test://test/node_modules/green/_e.scss"},
	})
}

func TestSCSSPackageDocumentLinks(t *testing.T) {
	resolver := packageLinkResolver{
		knownLinkResolver: knownLinkResolver{
			"test://test/node_modules/bar/package.json":         true,
			"test://test/node_modules/@foo/baz/package.json":    true,
			"test://test/node_modules/root-sass/package.json":   true,
			"test://test/node_modules/root-style/package.json":  true,
			"test://test/node_modules/bar-pattern/package.json": true,
			"test://test/node_modules/conditional/package.json": true,
		},
		files: map[string]string{
			"test://test/node_modules/bar/package.json": `{
				"exports": {
					".": { "sass": "./styles/index.scss" },
					"./colors": { "sass": "./styles/colors.scss" },
					"./colors.scss": { "sass": "./styles/colors.scss" }
				}
			}`,
			"test://test/node_modules/@foo/baz/package.json": `{
				"exports": {
					".": { "sass": "./styles/index.scss" },
					"./colors": { "sass": "./styles/colors.scss" },
					"./colors.scss": { "sass": "./styles/colors.scss" },
					"./button": { "sass": "./styles/button.scss" },
					"./button.scss": { "sass": "./styles/button.scss" }
				}
			}`,
			"test://test/node_modules/root-sass/package.json":  `{ "sass": "./styles/index.scss" }`,
			"test://test/node_modules/root-style/package.json": `{ "style": "./styles/index.scss" }`,
			"test://test/node_modules/bar-pattern/package.json": `{
				"exports": {
					"./*": { "sass": "./styles/*.scss" },
					"./*.scss": { "sass": "./styles/*.scss" }
				}
			}`,
			"test://test/node_modules/conditional/package.json": `{
				"exports": {
					".": { "default": "./index.js", "sass": "./_index.scss" }
				}
			}`,
		},
	}
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:bar"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 14), Target: "test://test/node_modules/bar/styles/index.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:bar/colors"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 21), Target: "test://test/node_modules/bar/styles/colors.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:bar/colors.scss"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 26), Target: "test://test/node_modules/bar/styles/colors.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:@foo/baz"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 19), Target: "test://test/node_modules/@foo/baz/styles/index.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:@foo/baz/button.scss"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 31), Target: "test://test/node_modules/@foo/baz/styles/button.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:root-sass"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 20), Target: "test://test/node_modules/root-sass/styles/index.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:root-style"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 21), Target: "test://test/node_modules/root-style/styles/index.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:bar-pattern/anything"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 31), Target: "test://test/node_modules/bar-pattern/styles/anything.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:bar-pattern/theme/dark.scss"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 38), Target: "test://test/node_modules/bar-pattern/styles/theme/dark.scss"},
	})
	assertLinksWithDocument(t, "test://test/about.scss", "scss", `@use "pkg:conditional"`, resolver, []lsp.DocumentLink{
		{Range: offsetRange(5, 22), Target: "test://test/node_modules/conditional/_index.scss"},
	})
}

func TestSCSSNavigationLinksPortedNamedCases(t *testing.T) {
	t.Run("Invalid SCSS partial file links", func(t *testing.T) {
		assertLinksWithDocument(t, "test://test/scss/linkFixture/non-existent/index.scss", "scss", `@import 'foo'`, knownLinkResolver{}, []lsp.DocumentLink{
			{Range: offsetRange(8, 13), Target: "test://test/scss/linkFixture/non-existent/foo"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/non-existent/index.scss", "scss", `@import './foo'`, knownLinkResolver{}, []lsp.DocumentLink{
			{Range: offsetRange(8, 15), Target: "test://test/scss/linkFixture/non-existent/foo"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/non-existent/index.scss", "scss", `@import './_foo'`, knownLinkResolver{}, []lsp.DocumentLink{
			{Range: offsetRange(8, 16), Target: "test://test/scss/linkFixture/non-existent/_foo"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/non-existent/index.scss", "scss", `@import './foo-baz'`, knownLinkResolver{}, []lsp.DocumentLink{
			{Range: offsetRange(8, 19), Target: "test://test/scss/linkFixture/non-existent/foo-baz"},
		})
	})
	t.Run("SCSS partial file dynamic links", func(t *testing.T) {
		resolver := knownLinkResolver{
			"test://test/scss/linkFixture/noUnderscore/foo.scss": true,
			"test://test/scss/linkFixture/underscore/_foo.scss":  true,
			"test://test/scss/linkFixture/both/foo.scss":         true,
			"test://test/scss/linkFixture/both/_foo.scss":        true,
			"test://test/scss/linkFixture/index/foo/index.scss":  true,
			"test://test/scss/linkFixture/index/bar/_index.scss": true,
		}
		assertLinksWithDocument(t, "test://test/scss/linkFixture/noUnderscore/index.scss", "scss", `@import 'foo'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 13), Target: "test://test/scss/linkFixture/noUnderscore/foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/underscore/index.scss", "scss", `@import 'foo'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 13), Target: "test://test/scss/linkFixture/underscore/_foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/underscore/index.scss", "scss", `@import 'foo.scss'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 18), Target: "test://test/scss/linkFixture/underscore/_foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/both/index.scss", "scss", `@import 'foo'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 13), Target: "test://test/scss/linkFixture/both/foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/both/index.scss", "scss", `@import '_foo'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 14), Target: "test://test/scss/linkFixture/both/_foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/index/index.scss", "scss", `@import 'foo'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 13), Target: "test://test/scss/linkFixture/index/foo/index.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/index/index.scss", "scss", `@import 'bar'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 13), Target: "test://test/scss/linkFixture/index/bar/_index.scss"},
		})
	})
	t.Run("SCSS straight links", func(t *testing.T) {
		assertLinksWithDocument(t, "test://test/test.scss", "scss", `@import 'foo.css'`, testDocumentContext{}, []lsp.DocumentLink{
			{Range: offsetRange(8, 17), Target: "test://test/foo.css"},
		})
		assertLinksWithDocument(t, "test://test/test.scss", "scss", `@import 'foo.scss' print;`, testDocumentContext{}, []lsp.DocumentLink{
			{Range: offsetRange(8, 18), Target: "test://test/foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/test.scss", "scss", `@import 'http://foo.com/foo.css'`, testDocumentContext{}, []lsp.DocumentLink{
			{Range: offsetRange(8, 32), Target: "http://foo.com/foo.css"},
		})
		assertLinksWithDocument(t, "test://test/test.scss", "scss", `@import url("foo.css") print;`, testDocumentContext{}, []lsp.DocumentLink{
			{Range: offsetRange(12, 21), Target: "test://test/foo.css"},
		})
	})
	t.Run("SCSS aliased links", func(t *testing.T) {
		resolver := aliasLinkResolver{
			aliases: map[string]string{
				"@SassStylesheet":   "/src/assets/styles.scss",
				"@NoUnderscoreDir/": "/scss/linkFixture/noUnderscore/",
				"@UnderscoreDir/":   "/scss/linkFixture/underscore/",
				"@BothDir/":         "/scss/linkFixture/both/",
			},
			fallback: knownLinkResolver{
				"test://test/src/assets/styles.scss":                 true,
				"test://test/scss/linkFixture/noUnderscore/foo.scss": true,
				"test://test/scss/linkFixture/underscore/_foo.scss":  true,
				"test://test/scss/linkFixture/both/foo.scss":         true,
				"test://test/scss/linkFixture/both/_foo.scss":        true,
			},
		}
		assertLinksWithDocument(t, "test://test/test.scss", "scss", `@import "@SassStylesheet"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 25), Target: "test://test/src/assets/styles.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/index.scss", "scss", `@import '@NoUnderscoreDir/foo'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 30), Target: "test://test/scss/linkFixture/noUnderscore/foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/index.scss", "scss", `@import '@UnderscoreDir/foo'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 28), Target: "test://test/scss/linkFixture/underscore/_foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/index.scss", "scss", `@import '@BothDir/foo'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 22), Target: "test://test/scss/linkFixture/both/foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/index.scss", "scss", `@import '@BothDir/_foo'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 23), Target: "test://test/scss/linkFixture/both/_foo.scss"},
		})
	})
	t.Run("SCSS module file links", func(t *testing.T) {
		resolver := knownLinkResolver{"test://test/scss/linkFixture/module/foo.scss": true}
		assertLinksWithDocument(t, "test://test/scss/linkFixture/module/index.scss", "scss", `@use './foo' as f`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(5, 12), Target: "test://test/scss/linkFixture/module/foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/module/index.scss", "scss", `@forward './foo' hide $private`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(9, 16), Target: "test://test/scss/linkFixture/module/foo.scss"},
		})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/module/index.scss", "scss", `@use 'sass:math'`, resolver, []lsp.DocumentLink{})
		assertLinksWithDocument(t, "test://test/scss/linkFixture/module/index.scss", "scss", `@use './non-existent'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(5, 21), Target: "test://test/scss/linkFixture/module/non-existent"},
		})
	})
	t.Run("SCSS empty path", func(t *testing.T) {
		assertLinksWithDocument(t, "test://test/test.scss", "scss", `#navigation { background: #3d3d3d url(gantry-media://gradient-overlay.png); }`, testDocumentContext{}, []lsp.DocumentLink{
			{Range: offsetRange(38, 73), Target: "gantry-media://gradient-overlay.png"},
		})
	})
	t.Run("SCSS node module resolving", func(t *testing.T) {
		resolver := knownLinkResolver{
			"test://test/linksTestFixtures/green/d.scss":                      true,
			"test://test/linksTestFixtures/node_modules/foo/hello.html":       true,
			"test://test/linksTestFixtures/node_modules/@foo/bar/_baz.scss":   true,
			"test://test/linksTestFixtures/node_modules/@foo/bar/_index.scss": true,
			"test://test/linksTestFixtures/node_modules/green/_e.scss":        true,
		}
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.scss", "scss", `html { background-image: url("~foo/hello.html")`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(29, 46), Target: "test://test/linksTestFixtures/node_modules/foo/hello.html"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.scss", "scss", `html { background-image: url("foo/hello.html")`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(29, 45), Target: "test://test/linksTestFixtures/node_modules/foo/hello.html"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.scss", "scss", `@use '@foo/bar/baz'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(5, 19), Target: "test://test/linksTestFixtures/node_modules/@foo/bar/_baz.scss"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.scss", "scss", `@use '@foo/bar'`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(5, 15), Target: "test://test/linksTestFixtures/node_modules/@foo/bar/_index.scss"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.scss", "scss", `@import "green/d"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 17), Target: "test://test/linksTestFixtures/green/d.scss"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.scss", "scss", `@import "./green/d"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 19), Target: "test://test/linksTestFixtures/green/d.scss"},
		})
		assertLinksWithDocument(t, "test://test/linksTestFixtures/about.scss", "scss", `@import "green/e"`, resolver, []lsp.DocumentLink{
			{Range: offsetRange(8, 17), Target: "test://test/linksTestFixtures/node_modules/green/_e.scss"},
		})
	})
	t.Run("SCSS node package resolving", func(t *testing.T) {
		resolver := testSCSSPackageResolver()
		tests := []struct {
			input  string
			end    int
			target string
		}{
			{`@use "pkg:bar"`, 14, "test://test/node_modules/bar/styles/index.scss"},
			{`@use "pkg:bar/colors"`, 21, "test://test/node_modules/bar/styles/colors.scss"},
			{`@use "pkg:bar/colors.scss"`, 26, "test://test/node_modules/bar/styles/colors.scss"},
			{`@use "pkg:@foo/baz"`, 19, "test://test/node_modules/@foo/baz/styles/index.scss"},
			{`@use "pkg:@foo/baz/colors"`, 26, "test://test/node_modules/@foo/baz/styles/colors.scss"},
			{`@use "pkg:@foo/baz/colors.scss"`, 31, "test://test/node_modules/@foo/baz/styles/colors.scss"},
			{`@use "pkg:@foo/baz/button"`, 26, "test://test/node_modules/@foo/baz/styles/button.scss"},
			{`@use "pkg:@foo/baz/button.scss"`, 31, "test://test/node_modules/@foo/baz/styles/button.scss"},
			{`@use "pkg:root-sass"`, 20, "test://test/node_modules/root-sass/styles/index.scss"},
			{`@use "pkg:root-style"`, 21, "test://test/node_modules/root-style/styles/index.scss"},
			{`@use "pkg:bar-pattern/anything"`, 31, "test://test/node_modules/bar-pattern/styles/anything.scss"},
			{`@use "pkg:bar-pattern/anything.scss"`, 36, "test://test/node_modules/bar-pattern/styles/anything.scss"},
			{`@use "pkg:bar-pattern/theme/dark.scss"`, 38, "test://test/node_modules/bar-pattern/styles/theme/dark.scss"},
			{`@use "pkg:conditional"`, 22, "test://test/node_modules/conditional/_index.scss"},
		}
		for _, tt := range tests {
			assertLinksWithDocument(t, "test://test/about.scss", "scss", tt.input, resolver, []lsp.DocumentLink{
				{Range: offsetRange(5, tt.end), Target: lsp.DocumentURI(tt.target)},
			})
		}
	})
}

func assertLinks(t *testing.T, input string, expected []lsp.DocumentLink) {
	t.Helper()
	assertLinksWithDocument(t, "test://test/test.css", "css", input, testDocumentContext{}, expected)
}

func assertLinksWithDocument(t *testing.T, uri lsp.DocumentURI, languageID string, input string, resolver DocumentResolver, expected []lsp.DocumentLink) {
	t.Helper()
	document := lsp.NewTextDocument(uri, languageID, 0, input)
	actual, err := FindDocumentLinks(context.Background(), document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", input, actual, expected)
	}
}

type knownLinkResolver map[string]bool

func (r knownLinkResolver) ResolveReference(ref, baseURL string) (string, bool) {
	if hasURIProtocol(ref) {
		return ref, r[ref]
	}
	target := ref
	if strings.HasPrefix(ref, "./") {
		target = parentCompletionURI(baseURL) + strings.TrimPrefix(ref, "./")
	} else if strings.HasPrefix(ref, "../") {
		target = parentCompletionURI(parentCompletionURI(baseURL)) + strings.TrimPrefix(ref, "../")
	} else if strings.HasPrefix(ref, "/") {
		target = "test://test" + ref
	} else {
		target = parentCompletionURI(baseURL) + ref
	}
	return target, r[target]
}

type aliasLinkResolver struct {
	aliases  map[string]string
	fallback DocumentResolver
}

func (r aliasLinkResolver) ResolveReference(ref, baseURL string) (string, bool) {
	for alias, target := range r.aliases {
		if ref == alias || strings.HasPrefix(ref, alias) {
			return r.fallback.ResolveReference(target+strings.TrimPrefix(ref, alias), baseURL)
		}
	}
	return r.fallback.ResolveReference(ref, baseURL)
}

type cssModuleLinkResolver struct {
	root  string
	known knownLinkResolver
}

func (r cssModuleLinkResolver) ResolveReference(ref, baseURL string) (string, bool) {
	localTarget, localOK := r.known.ResolveReference(ref, baseURL)
	if localOK {
		return localTarget, true
	}
	if strings.HasPrefix(ref, "~") && !strings.HasPrefix(ref, "~/") {
		target := joinPathURI(r.root, "node_modules/"+strings.TrimPrefix(ref, "~"))
		return target, r.known[target]
	}
	if !strings.HasPrefix(ref, ".") && !strings.HasPrefix(ref, "/") && !hasURIProtocol(ref) {
		target := joinPathURI(r.root, "node_modules/"+ref)
		return target, r.known[target]
	}
	return localTarget, localOK
}

type packageLinkResolver struct {
	knownLinkResolver
	files map[string]string
}

func (r packageLinkResolver) ReadFile(ctx context.Context, uri string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return r.files[uri], nil
}

func testSCSSPackageResolver() packageLinkResolver {
	return packageLinkResolver{
		knownLinkResolver: knownLinkResolver{
			"test://test/node_modules/bar/package.json":         true,
			"test://test/node_modules/@foo/baz/package.json":    true,
			"test://test/node_modules/root-sass/package.json":   true,
			"test://test/node_modules/root-style/package.json":  true,
			"test://test/node_modules/bar-pattern/package.json": true,
			"test://test/node_modules/conditional/package.json": true,
		},
		files: map[string]string{
			"test://test/node_modules/bar/package.json": `{
				"exports": {
					".": { "sass": "./styles/index.scss" },
					"./colors": { "sass": "./styles/colors.scss" },
					"./colors.scss": { "sass": "./styles/colors.scss" }
				}
			}`,
			"test://test/node_modules/@foo/baz/package.json": `{
				"exports": {
					".": { "sass": "./styles/index.scss" },
					"./colors": { "sass": "./styles/colors.scss" },
					"./colors.scss": { "sass": "./styles/colors.scss" },
					"./button": { "sass": "./styles/button.scss" },
					"./button.scss": { "sass": "./styles/button.scss" }
				}
			}`,
			"test://test/node_modules/root-sass/package.json":  `{ "sass": "./styles/index.scss" }`,
			"test://test/node_modules/root-style/package.json": `{ "style": "./styles/index.scss" }`,
			"test://test/node_modules/bar-pattern/package.json": `{
				"exports": {
					"./*": { "sass": "./styles/*.scss" },
					"./*.scss": { "sass": "./styles/*.scss" }
				}
			}`,
			"test://test/node_modules/conditional/package.json": `{
				"exports": {
					".": { "default": "./index.js", "sass": "./_index.scss" }
				}
			}`,
		},
	}
}

type testDocumentContext struct{}

func (testDocumentContext) ResolveReference(ref, baseURL string) (string, bool) {
	ref = strings.TrimPrefix(ref, "./")
	if strings.HasPrefix(ref, "../") {
		return "test://" + strings.TrimPrefix(ref, "../"), true
	}
	return "test://test/" + ref, true
}
