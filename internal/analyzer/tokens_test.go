package analyzer_test

import (
	"context"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestTiktokenCounter_emptyString_returnsZero(t *testing.T) {
	c := analyzer.NewTiktokenCounter()
	n, err := c.CountTokens(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestTiktokenCounter_nonEmptyString_returnsPositive(t *testing.T) {
	c := analyzer.NewTiktokenCounter()
	n, err := c.CountTokens(context.Background(), "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if n <= 0 {
		t.Errorf("expected positive token count, got %d", n)
	}
}

func TestTiktokenCounter_longerString_moreTokens(t *testing.T) {
	c := analyzer.NewTiktokenCounter()
	short, _ := c.CountTokens(context.Background(), "hello")
	long, _ := c.CountTokens(context.Background(), "hello world this is a longer sentence with many words")
	if long <= short {
		t.Errorf("expected longer string to have more tokens: short=%d long=%d", short, long)
	}
}
