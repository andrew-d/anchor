// Package timesleep defines an analysis pass that reports time.Sleep calls
// in test files that are not inside a testing/synctest bubble.
package timesleep

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "timesleep",
	Doc:      "reports time.Sleep in tests outside synctest.Test bubbles",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// Collect position ranges of function literals passed to synctest.Test.
	// Any time.Sleep lexically inside one of these ranges is allowed.
	type posRange struct {
		pos, end token.Pos
	}
	var bubbles []posRange

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	insp.Preorder(nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if !isSynctestTest(pass, call) || len(call.Args) < 2 {
			return
		}
		if fl, ok := call.Args[1].(*ast.FuncLit); ok {
			bubbles = append(bubbles, posRange{fl.Pos(), fl.End()})
		}
	})

	// Find time.Sleep calls in test files that fall outside every bubble.
	insp.Preorder(nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if !isTimeSleep(pass, call) {
			return
		}

		// Only check test files.
		pos := pass.Fset.Position(call.Pos())
		if !strings.HasSuffix(pos.Filename, "_test.go") {
			return
		}

		for _, b := range bubbles {
			if call.Pos() >= b.pos && call.End() <= b.end {
				return
			}
		}

		pass.ReportRangef(call, "time.Sleep in test outside synctest bubble")
	})

	return nil, nil
}

func isTimeSleep(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sleep" {
		return false
	}
	obj := pass.TypesInfo.Uses[sel.Sel]
	if obj == nil {
		return false
	}
	pkg := obj.Pkg()
	return pkg != nil && pkg.Path() == "time"
}

func isSynctestTest(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Test" {
		return false
	}
	obj := pass.TypesInfo.Uses[sel.Sel]
	if obj == nil {
		return false
	}
	pkg := obj.Pkg()
	return pkg != nil && pkg.Path() == "testing/synctest"
}
