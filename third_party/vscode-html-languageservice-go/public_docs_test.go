package htmlservice

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicTypeDocComments(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				if !typeSpec.Name.IsExported() {
					continue
				}
				if gen.Doc == nil {
					t.Errorf("%s:%d exported type %s is missing a doc comment", name, fset.Position(typeSpec.Pos()).Line, typeSpec.Name.Name)
					continue
				}
				text := strings.TrimSpace(gen.Doc.Text())
				if !strings.HasPrefix(text, typeSpec.Name.Name+" ") && text != typeSpec.Name.Name {
					t.Errorf("%s:%d exported type %s doc comment must start with the type name", name, fset.Position(typeSpec.Pos()).Line, typeSpec.Name.Name)
				}
			}
		}
	}
}
