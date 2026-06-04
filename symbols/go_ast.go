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

	emitRef := func(name, pkg string, pos token.Pos) {
		add(name, pkg, SymTypeRef, pos)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			if v.Name != nil {
				add(v.Name.Name, "", SymDef, v.Pos())
			}
			// v1.1.0: emit SymTypeRef for parameter and return types.
			if v.Type != nil {
				if v.Type.Params != nil {
					for _, f := range v.Type.Params.List {
						emitTypeRefs(f.Type, emitRef)
					}
				}
				if v.Type.Results != nil {
					for _, f := range v.Type.Results.List {
						emitTypeRefs(f.Type, emitRef)
					}
				}
			}
		case *ast.TypeSpec:
			if v.Name != nil {
				add(v.Name.Name, "", SymDef, v.Pos())
			}
		case *ast.GenDecl:
			// v1.1.0: emit SymTypeRef for explicitly typed var declarations.
			if v.Tok == token.VAR {
				for _, sp := range v.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok || vs.Type == nil {
						continue
					}
					emitTypeRefs(vs.Type, emitRef)
				}
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

// emitTypeRefs walks a type expression and calls add for each type identifier
// it finds. Handles common Go type shapes:
//
//	*T           → emit T
//	[]T          → emit T
//	map[K]V      → emit K + V
//	chan T        → emit T
//	pkg.T         → emit T with pkg set
//	func(P) R    → recurse into P + R
//	...T          → emit T (variadic)
//
// Struct/interface literal types are skipped to avoid double-emission with
// the SymDef already emitted from the TypeSpec visitor.
func emitTypeRefs(t ast.Expr, add func(name, pkg string, pos token.Pos)) {
	switch tt := t.(type) {
	case *ast.Ident:
		add(tt.Name, "", tt.Pos())
	case *ast.StarExpr:
		emitTypeRefs(tt.X, add)
	case *ast.SelectorExpr:
		if x, ok := tt.X.(*ast.Ident); ok {
			add(tt.Sel.Name, x.Name, tt.Pos())
		} else {
			add(tt.Sel.Name, "", tt.Pos())
		}
	case *ast.ArrayType:
		emitTypeRefs(tt.Elt, add)
	case *ast.MapType:
		emitTypeRefs(tt.Key, add)
		emitTypeRefs(tt.Value, add)
	case *ast.ChanType:
		emitTypeRefs(tt.Value, add)
	case *ast.FuncType:
		if tt.Params != nil {
			for _, f := range tt.Params.List {
				emitTypeRefs(f.Type, add)
			}
		}
		if tt.Results != nil {
			for _, f := range tt.Results.List {
				emitTypeRefs(f.Type, add)
			}
		}
	case *ast.Ellipsis:
		if tt.Elt != nil {
			emitTypeRefs(tt.Elt, add)
		}
	case *ast.InterfaceType, *ast.StructType:
		// Skip — these define types, not reference them.
	}
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
