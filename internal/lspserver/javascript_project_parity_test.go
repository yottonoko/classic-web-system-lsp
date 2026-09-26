package lspserver

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJavaScriptModuleExtensionsMatchTypeScriptProjectFiles(t *testing.T) {
	for _, path := range []string{
		"file.js", "file.mjs", "file.cjs", "file.jsx", "file.ts", "file.tsx", "file.mts", "file.cts", "file.d.ts",
	} {
		if !isJavaScriptModuleFile(path) {
			t.Errorf("isJavaScriptModuleFile(%q) = false", path)
		}
	}
	for _, path := range []string{
		"jsconfig.json", "tsconfig.json", "package.json", "node_modules/@types/node/index.d.ts",
	} {
		if !isJavaScriptProjectFile(path) {
			t.Errorf("isJavaScriptProjectFile(%q) = false", path)
		}
	}
	if isJavaScriptModuleFile("file.css") || isJavaScriptProjectFile("file.css") {
		t.Fatal("CSS file was treated as a JavaScript project file")
	}
}

func TestJavaScriptModuleResolutionUsesPackageTypesAndIndexExtensions(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "src", "main.tsx")
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		t.Fatal(err)
	}
	packageDirectory := filepath.Join(root, "node_modules", "sample-package")
	if err := os.MkdirAll(packageDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	typesFile := filepath.Join(packageDirectory, "types", "index.d.ts")
	if err := os.MkdirAll(filepath.Dir(typesFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDirectory, "package.json"), []byte(`{"types":"types/index.d.ts"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(typesFile, []byte("export declare const answer: number;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.closeDiskAnalysisCache()
	resolved, ok := server.resolveJavaScriptModulePath(root, current, "sample-package")
	if !ok || filepath.Clean(resolved) != filepath.Clean(typesFile) {
		t.Fatalf("resolveJavaScriptModulePath() = %q, %v; want %q", resolved, ok, typesFile)
	}

	jsxFile := filepath.Join(root, "src", "component.jsx")
	if err := os.WriteFile(jsxFile, []byte("export const Component = () => null;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, ok = server.resolveJavaScriptModulePath(root, current, "./component")
	if !ok || filepath.Clean(resolved) != filepath.Clean(jsxFile) {
		t.Fatalf("extension resolution = %q, %v; want %q", resolved, ok, jsxFile)
	}
}

func TestJavaScriptProjectTypesRespectTsconfigAndAmbientTypes(t *testing.T) {
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@types", "jquery"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"compilerOptions":{"types":[]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if server.javascriptJQueryCompletionEnabled() {
		t.Fatal("explicit jsconfig compilerOptions.types=[] enabled ambient jquery")
	}
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"compilerOptions":{"types":["jquery"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !server.javascriptJQueryCompletionEnabled() {
		t.Fatal("jsconfig compilerOptions.types=[jquery] did not enable jquery")
	}
}
