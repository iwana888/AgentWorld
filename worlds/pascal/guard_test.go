package pascal

import (
	"testing"
)

func TestGuard_PascalWriteBlocked(t *testing.T) {
	g := NewGuard()
	// TEST_FILE_IMMUTABLE：禁止写 test_*.pas
	r := g.Check(&ToolCall{Action: "write_file", Args: map[string]string{"path": "tests/test_calc.pas"}}, GuardState{}, "calc.pas")
	if r.Allowed {
		t.Fatal("expected DENY for writing test file")
	}
	if r.Rule != "TEST_FILE_IMMUTABLE" {
		t.Fatalf("expected TEST_FILE_IMMUTABLE, got %s", r.Rule)
	}
}

func TestGuard_UnitNameMismatch(t *testing.T) {
	g := NewGuard()
	// UNIT_NAME_MATCH：unit Foo 但文件叫 bar.pas → DENY
	r := g.Check(&ToolCall{Action: "write_file", Args: map[string]string{
		"path":    "bar.pas",
		"content": "unit Foo;\nbegin end.",
	}}, GuardState{}, "bar.pas")
	if r.Allowed {
		t.Fatal("expected DENY for unit name mismatch")
	}
	if r.Rule != "UNIT_NAME_MATCH" {
		t.Fatalf("expected UNIT_NAME_MATCH, got %s", r.Rule)
	}
}

func TestGuard_LegalWriteAllowed(t *testing.T) {
	g := NewGuard()
	r := g.Check(&ToolCall{Action: "write_file", Args: map[string]string{
		"path":    "calc.pas",
		"content": "unit calc;\nbegin end.",
	}}, GuardState{}, "calc.pas")
	if !r.Allowed {
		t.Fatalf("expected ALLOW for matching unit name, got DENY (%s)", r.Rule)
	}
}

func TestGuard_CompileRequiredBeforeSubmit(t *testing.T) {
	g := NewGuard()
	// 改过生产代码但未编译 → MUST_COMPILE DENY
	r := g.Check(&ToolCall{Action: intentSubmit}, GuardState{ModifiedSrc: true, CompileOK: false}, "calc.pas")
	if r.Allowed {
		t.Fatal("expected DENY for submit without compile")
	}
	if r.Rule != "MUST_COMPILE" {
		t.Fatalf("expected MUST_COMPILE, got %s", r.Rule)
	}
	// 改过且已编译 → ALLOW
	r2 := g.Check(&ToolCall{Action: intentSubmit}, GuardState{ModifiedSrc: true, CompileOK: true}, "calc.pas")
	if !r2.Allowed {
		t.Fatal("expected ALLOW when compiled")
	}
}
