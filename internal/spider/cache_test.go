package spider

import (
	"strings"
	"testing"
)

func TestURLToFilename_isStable(t *testing.T) {
	a := URLToFilename("https://docs.example.com/intro")
	b := URLToFilename("https://docs.example.com/intro")
	if a != b {
		t.Errorf("URLToFilename is not stable: %q != %q", a, b)
	}
}

func TestURLToFilename_differsAcrossURLs(t *testing.T) {
	a := URLToFilename("https://docs.example.com/intro")
	b := URLToFilename("https://docs.example.com/reference")
	if a == b {
		t.Error("URLToFilename returned same name for different URLs")
	}
}

func TestURLToFilename_hasMDExtension(t *testing.T) {
	name := URLToFilename("https://docs.example.com/intro")
	if !strings.HasSuffix(name, ".md") {
		t.Errorf("expected .md suffix, got %q", name)
	}
}
