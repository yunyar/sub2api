//go:build unit

package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandlerIngressDoesNotUseLegacyForwardedIPResolver(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	var legacyCalls []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "GetClientIP" {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if ok && pkg.Name == "ip" {
				legacyCalls = append(legacyCalls, fset.Position(call.Pos()).String())
			}
			return true
		})
	}

	require.Empty(t, legacyCalls, "handler ingress must not persist or act on raw forwarded-header IPs")
}
