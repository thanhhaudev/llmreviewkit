package symbols

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// GoASTExtractor uses stdlib go/ast to extract symbols from Go source files.
// This is the canonical Go parser — 100% accurate for Go syntax.
type GoASTExtractor struct{}

func (e *GoASTExtractor) Extract(path string, content []byte) []Symbol {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return nil
	}

	var syms []Symbol
	add := func(name, pkg string, kind SymbolKind, pos token.Pos) {
		syms = append(syms, Symbol{
			Name: name,
			Pkg:  pkg,
			Kind: kind,
			File: path,
			Line: fset.Position(pos).Line,
		})
	}

	for _, imp := range file.Imports {
		raw := imp.Path.Value
		stripped := strings.Trim(raw, "\"")
		add(stripped, "", SymImport, imp.Pos())
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			if v.Name != nil {
				add(v.Name.Name, "", SymDef, v.Pos())
			}
		case *ast.TypeSpec:
			if v.Name != nil {
				add(v.Name.Name, "", SymDef, v.Pos())
			}
		case *ast.CallExpr:
			switch fn := v.Fun.(type) {
			case *ast.SelectorExpr:
				pkg := selectorReceiverName(fn.X)
				add(fn.Sel.Name, pkg, SymCall, v.Pos())
			case *ast.Ident:
				add(fn.Name, "", SymCall, v.Pos())
			}
		}
		return true
	})
	return syms
}

// selectorReceiverName returns the name to record as Symbol.Pkg for
// the receiver in a SelectorExpr-based call. Handles two shapes:
//
//	x.Method()           → "x"
//	x.y.Method()         → "y" (innermost SelectorExpr's Sel)
//
// Returns "" for anything else (e.g. f().Method, (&T{}).Method) so
// we don't emit a misleading Pkg value.
func selectorReceiverName(x ast.Expr) string {
	switch n := x.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.SelectorExpr:
		return n.Sel.Name
	}
	return ""
}
