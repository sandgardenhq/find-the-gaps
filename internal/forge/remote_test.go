package forge

import "testing"

func TestNormalizeRemote(t *testing.T) {
	cases := []struct {
		raw       string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"https://github.com/foo/bar.git", "github.com", "foo", "bar", false},
		{"https://github.com/foo/bar", "github.com", "foo", "bar", false},
		{"git@github.com:foo/bar.git", "github.com", "foo", "bar", false},
		{"git@gitlab.com:group/proj.git", "gitlab.com", "group", "proj", false},
		{"ssh://git@github.com/foo/bar.git", "github.com", "foo", "bar", false},
		{"https://GitHub.com/Foo/Bar.git", "github.com", "Foo", "Bar", false}, // host lowercased, owner/repo preserved
		{"file:///tmp/foo", "", "", "", true},
		{"", "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := NormalizeRemote(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Host != tc.wantHost || got.Owner != tc.wantOwner || got.Repo != tc.wantRepo {
				t.Fatalf("got %+v want host=%s owner=%s repo=%s",
					got, tc.wantHost, tc.wantOwner, tc.wantRepo)
			}
		})
	}
}
