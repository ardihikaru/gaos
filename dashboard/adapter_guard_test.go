package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// The guards on the adapter seam that outlived the CLI adapter (M14.7): the
// configuration's valid and refused states, the zero-value Cleared, and BR-33's
// syntax-tree check.

// TestAnUnconfiguredAdapterIsAValidState. Mode off is the local stack.
func TestAnUnconfiguredAdapterIsAValidState(t *testing.T) {
	a, err := selectAdapter(ModelConfig{Mode: ModeOff})
	if err != nil {
		t.Fatalf("mode off refused to build: %v; an unconfigured dependency is the local stack and a valid state, exactly as handleHealth has always treated an unconfigured database", err)
	}
	if a != nil {
		t.Fatalf("mode off built adapter %v, want nil", a)
	}
	if a, err := selectAdapter(ModelConfig{}); err != nil || a != nil {
		t.Fatalf("an empty ModelConfig gave (%v, %v), want (nil, nil); an upgrade that refused to boot until a new variable was set would take every running dashboard down", a, err)
	}

	// The retired mode refuses with its reason rather than quietly mapping to
	// the gateway: where an operator's code goes must not change unannounced.
	if _, err := selectAdapter(ModelConfig{Mode: ModeCLI}); err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("GAOS_MODEL_MODE=cli gave %v; the CLI path was retired (D-055) and must refuse the boot saying so", err)
	}
	if _, err := selectAdapter(ModelConfig{Mode: ModeAIM}); err == nil {
		t.Fatal("gateway mode with no URL built an adapter; a half-configured gateway is a deployment error, not a run to fail later")
	}
	if _, err := selectAdapter(ModelConfig{Mode: ModeAPI}); err == nil {
		t.Fatal("mode api built an adapter; T-058 is not built, and an operator who asked for a metered adapter must not silently get another")
	}
}

// TestTheAdapterRefusesAPacketTheEgressPassNeverCleared is the belt behind the
// type discipline's braces.
func TestTheAdapterRefusesAPacketTheEgressPassNeverCleared(t *testing.T) {
	f, a := newFakeAIM(t)
	if _, err := a.Dispatch(context.Background(), Cleared{}); err == nil {
		t.Fatal("the adapter dispatched a zero-value Cleared; BR-32 says nothing reaches a model provider without passing the egress check first, and a type is only most of that guarantee")
	}
	if len(f.bodies) != 0 {
		t.Fatalf("a zero-value Cleared reached the gateway as %d request(s)", len(f.bodies))
	}
}

// TestNothingAboveTheAdapterBranchesOnTheInvocationMode is BR-33 made
// checkable rather than trusted: "the execution mode may be recorded and never
// branched on". TaskRun.invocation exists so the prohibition can be checked,
// and this is the check.
func TestNothingAboveTheAdapterBranchesOnTheInvocationMode(t *testing.T) {
	fset := token.NewFileSet()
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range sources {
		switch name {
		case "adapter.go", "adapter_aimanager.go", "modelconfig.go",
			"adapter_guard_test.go", "aimanager_test.go":
			continue // the seam itself, and the one place that selects it
		}
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parsing %s: %v", name, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var x ast.Expr
			switch e := n.(type) {
			case *ast.BinaryExpr:
				if (e.Op == token.EQL || e.Op == token.NEQ) && (isInvocationConst(e.X) || isInvocationConst(e.Y)) {
					x = e
				}
			case *ast.SwitchStmt:
				if e.Tag != nil && isInvocationField(e.Tag) {
					x = e.Tag
				}
			}
			if x != nil {
				pos := fset.Position(x.Pos())
				t.Errorf("%s:%d branches on the invocation mode outside the adapter; BR-33 says the mode may be recorded and never branched on, and TaskRun.invocation exists so that prohibition can be checked rather than trusted", name, pos.Line)
			}
			return true
		})
	}
}

func isInvocationConst(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && (id.Name == "InvocationCLI" || id.Name == "InvocationAPI" || id.Name == "InvocationGateway")
}

func isInvocationField(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel != nil && sel.Sel.Name == "Invocation"
}
