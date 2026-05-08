package cli

import (
	"reflect"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestSupportedLanguages_dropsGenericEntry(t *testing.T) {
	scan := &scanner.ProjectScan{Languages: []string{"Go", "Generic", "Python"}}
	got := supportedLanguages(scan)
	want := []string{"Go", "Python"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("supportedLanguages() = %v, want %v", got, want)
	}
}

func TestSupportedLanguages_returnsEmptyWhenOnlyGeneric(t *testing.T) {
	scan := &scanner.ProjectScan{Languages: []string{"Generic"}}
	if got := supportedLanguages(scan); len(got) != 0 {
		t.Fatalf("supportedLanguages() = %v, want []", got)
	}
}

func TestSupportedLanguages_returnsEmptyWhenNoLanguages(t *testing.T) {
	scan := &scanner.ProjectScan{Languages: nil}
	if got := supportedLanguages(scan); len(got) != 0 {
		t.Fatalf("supportedLanguages() = %v, want []", got)
	}
}
