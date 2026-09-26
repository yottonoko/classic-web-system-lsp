package beautify

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportedSymbolsHaveDocComments(t *testing.T) {
	root := "."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git", ".cache":
			return filepath.SkipDir
		}
		return checkPackageDocComments(t, path)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkPackageDocComments(t *testing.T, dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return err
	}
	hasSource := false
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			hasSource = true
			break
		}
	}
	if !hasSource {
		return nil
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return err
	}
	for _, pkg := range pkgs {
		if pkg.Name != "main" && !hasPackageDoc(pkg) {
			t.Errorf("%s: package %s has no package comment", dir, pkg.Name)
		}
		for _, file := range pkg.Files {
			checkFileDocComments(t, fset, file)
		}
	}
	return nil
}

func hasPackageDoc(pkg *ast.Package) bool {
	for _, file := range pkg.Files {
		if file.Doc != nil {
			return true
		}
	}
	return false
}

func checkFileDocComments(t *testing.T, fset *token.FileSet, file *ast.File) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			checkGenDeclDocComments(t, fset, d)
		case *ast.FuncDecl:
			if d.Name != nil && d.Name.IsExported() && d.Doc == nil {
				t.Errorf("%s: exported function or method %s has no doc comment", fset.Position(d.Pos()), d.Name.Name)
			}
		}
	}
}

func checkGenDeclDocComments(t *testing.T, fset *token.FileSet, decl *ast.GenDecl) {
	if decl.Tok != token.CONST && decl.Tok != token.VAR && decl.Tok != token.TYPE {
		return
	}
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if s.Name.IsExported() && decl.Doc == nil && s.Doc == nil {
				t.Errorf("%s: exported type %s has no doc comment", fset.Position(s.Pos()), s.Name.Name)
			}
			if s.Name.IsExported() {
				if structType, ok := s.Type.(*ast.StructType); ok {
					checkStructFieldDocComments(t, fset, s.Name.Name, structType)
				}
			}
		case *ast.ValueSpec:
			for _, name := range s.Names {
				if name.IsExported() && decl.Doc == nil && s.Doc == nil {
					t.Errorf("%s: exported value %s has no doc comment", fset.Position(name.Pos()), name.Name)
				}
			}
		}
	}
}

func checkStructFieldDocComments(t *testing.T, fset *token.FileSet, typeName string, structType *ast.StructType) {
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			if name.IsExported() && field.Doc == nil {
				t.Errorf("%s: exported field %s.%s has no doc comment", fset.Position(name.Pos()), typeName, name.Name)
			}
		}
		if len(field.Names) == 0 {
			if name := embeddedFieldName(field.Type); name != "" && ast.IsExported(name) && field.Doc == nil {
				t.Errorf("%s: exported embedded field %s.%s has no doc comment", fset.Position(field.Pos()), typeName, name)
			}
		}
	}
}

func embeddedFieldName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return embeddedFieldName(e.X)
	case *ast.SelectorExpr:
		return e.Sel.Name
	default:
		return ""
	}
}
