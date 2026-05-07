package doctor

import (
	"context"
	"strings"
	"testing"
)

func TestRequire_AllToolsPresent_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	writeFakeBin(t, dir, "hugo", "hugo v0.154.5")
	t.Setenv("PATH", dir)

	err := Require(context.Background(), Precheck{
		Command: "ftg analyze",
		Tools:   []string{"mdfetch", "hugo"},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestRequire_MdfetchMissing_NamesMdfetchAndHint(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "hugo", "hugo v0.154.5")
	t.Setenv("PATH", dir)

	err := Require(context.Background(), Precheck{
		Command: "ftg analyze",
		Tools:   []string{"mdfetch", "hugo"},
	})
	if err == nil {
		t.Fatal("err = nil, want missing-tool error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ftg analyze needs") {
		t.Errorf("error should name calling command; got %q", msg)
	}
	if !strings.Contains(msg, "mdfetch") {
		t.Errorf("error should name mdfetch; got %q", msg)
	}
	if !strings.Contains(msg, "not found") {
		t.Errorf("error should say tool was not found; got %q", msg)
	}
	mdfetchTool := findRequiredTool(t, "mdfetch")
	if !strings.Contains(msg, mdfetchTool.InstallHint) {
		t.Errorf("error should include mdfetch install hint %q; got %q", mdfetchTool.InstallHint, msg)
	}
	if strings.Contains(msg, "hugo —") || strings.Contains(msg, "hugo -") {
		t.Errorf("error should not list hugo (it is present); got %q", msg)
	}
}

func TestRequire_BothMissing_ListsBothInOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	err := Require(context.Background(), Precheck{
		Command: "ftg analyze",
		Tools:   []string{"mdfetch", "hugo"},
	})
	if err == nil {
		t.Fatal("err = nil, want missing-tool error")
	}
	msg := err.Error()
	mdfetchIdx := strings.Index(msg, "mdfetch")
	hugoIdx := strings.Index(msg, "hugo")
	if mdfetchIdx < 0 || hugoIdx < 0 {
		t.Fatalf("error should name both tools; got %q", msg)
	}
	if mdfetchIdx >= hugoIdx {
		t.Errorf("tools should appear in the order requested (mdfetch then hugo); got %q", msg)
	}
}

func TestRequire_HugoOnly_DoesNotMentionMdfetch(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	t.Setenv("PATH", dir)

	err := Require(context.Background(), Precheck{
		Command: "ftg render",
		Tools:   []string{"hugo"},
	})
	if err == nil {
		t.Fatal("err = nil, want missing-tool error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ftg render needs") {
		t.Errorf("error should name calling command; got %q", msg)
	}
	if !strings.Contains(msg, "hugo") {
		t.Errorf("error should name hugo; got %q", msg)
	}
	if strings.Contains(msg, "mdfetch") {
		t.Errorf("error should not mention mdfetch when not requested; got %q", msg)
	}
}

func TestRequire_Suffix_AppendedToError(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	t.Setenv("PATH", dir)

	err := Require(context.Background(), Precheck{
		Command: "ftg analyze",
		Tools:   []string{"mdfetch", "hugo"},
		Suffix:  "Pass --no-site to skip Hugo.",
	})
	if err == nil {
		t.Fatal("err = nil, want missing-tool error")
	}
	if !strings.Contains(err.Error(), "Pass --no-site to skip Hugo.") {
		t.Errorf("error should include suffix; got %q", err.Error())
	}
}

func TestRequire_UnknownToolName_ReturnsError(t *testing.T) {
	err := Require(context.Background(), Precheck{
		Command: "ftg analyze",
		Tools:   []string{"not-a-real-tool"},
	})
	if err == nil {
		t.Fatal("err = nil, want unknown-tool error")
	}
	if !strings.Contains(err.Error(), "not-a-real-tool") {
		t.Errorf("error should name the unknown tool; got %q", err.Error())
	}
}

func findRequiredTool(t *testing.T, name string) Tool {
	t.Helper()
	for _, tool := range RequiredTools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in RequiredTools", name)
	return Tool{}
}
