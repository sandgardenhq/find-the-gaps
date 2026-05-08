package forge

import "testing"

func TestParseURL(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantRef   string
		wantSub   string
		wantWiki  bool
	}{
		{
			name:      "repo root",
			raw:       "https://github.com/foo/bar",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
		},
		{
			name:      "tree with subpath",
			raw:       "https://github.com/foo/bar/tree/main/docs",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
			wantRef:   "main",
			wantSub:   "docs",
		},
		{
			name:      "blob single file",
			raw:       "https://github.com/foo/bar/blob/main/README.md",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
			wantRef:   "main",
			wantSub:   "README.md",
		},
		{
			name:      "wiki",
			raw:       "https://github.com/foo/bar/wiki",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
			wantWiki:  true,
		},
		{
			name:      "trailing .git stripped",
			raw:       "https://github.com/foo/bar.git",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
		},
		{
			name:      "trailing slash on repo root",
			raw:       "https://github.com/foo/bar/",
			wantOwner: "foo",
			wantRepo:  "bar",
			wantHost:  "github.com",
		},
		{
			name:      "host with port stripped",
			raw:       "https://gitlab.example.com:8443/foo/bar/tree/main/docs",
			wantHost:  "gitlab.example.com",
			wantOwner: "foo",
			wantRepo:  "bar",
			wantRef:   "main",
			wantSub:   "docs",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseURL(tc.raw)
			if err != nil {
				t.Fatalf("ParseURL(%q) returned error: %v", tc.raw, err)
			}
			if got.Host != tc.wantHost || got.Owner != tc.wantOwner || got.Repo != tc.wantRepo {
				t.Fatalf("got %+v want host=%s owner=%s repo=%s",
					got, tc.wantHost, tc.wantOwner, tc.wantRepo)
			}
			if got.Ref != tc.wantRef || got.Subpath != tc.wantSub || got.IsWiki != tc.wantWiki {
				t.Fatalf("got %+v want ref=%s sub=%s wiki=%v",
					got, tc.wantRef, tc.wantSub, tc.wantWiki)
			}
		})
	}
}

func TestParseURL_rejectsNonForgePaths(t *testing.T) {
	// Only owner, no repo
	if _, err := ParseURL("https://github.com/foo"); err == nil {
		t.Fatal("expected error for owner-only URL")
	}
	// Empty path
	if _, err := ParseURL("https://github.com/"); err == nil {
		t.Fatal("expected error for empty path")
	}
	// Malformed URL
	if _, err := ParseURL("https://example.com/\x7f"); err == nil {
		t.Fatal("expected error for malformed URL")
	}
}
