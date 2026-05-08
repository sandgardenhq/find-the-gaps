package forge

import "testing"

func TestSameRepo(t *testing.T) {
	cases := []struct {
		name   string
		docs   URL
		remote Remote
		want   bool
	}{
		{
			name:   "exact match",
			docs:   URL{Host: "github.com", Owner: "foo", Repo: "bar"},
			remote: Remote{Host: "github.com", Owner: "foo", Repo: "bar"},
			want:   true,
		},
		{
			name:   "case-insensitive owner/repo",
			docs:   URL{Host: "github.com", Owner: "Foo", Repo: "Bar"},
			remote: Remote{Host: "github.com", Owner: "foo", Repo: "bar"},
			want:   true,
		},
		{
			name:   "host mismatch",
			docs:   URL{Host: "github.com", Owner: "foo", Repo: "bar"},
			remote: Remote{Host: "gitlab.com", Owner: "foo", Repo: "bar"},
			want:   false,
		},
		{
			name:   "owner mismatch",
			docs:   URL{Host: "github.com", Owner: "foo", Repo: "bar"},
			remote: Remote{Host: "github.com", Owner: "baz", Repo: "bar"},
			want:   false,
		},
	}
	// Cross-form: docs URL has https with non-443 port, origin is scp-style
	// (no port). After Hostname()-stripping at the parse boundary, both hosts
	// equal "gitlab.example.com" and SameRepo must report true.
	t.Run("cross-form host with port matches scp-style", func(t *testing.T) {
		docs, err := ParseURL("https://gitlab.example.com:8443/foo/bar/tree/main/docs")
		if err != nil {
			t.Fatalf("ParseURL: %v", err)
		}
		remote, err := NormalizeRemote("git@gitlab.example.com:foo/bar.git")
		if err != nil {
			t.Fatalf("NormalizeRemote: %v", err)
		}
		if !SameRepo(docs, remote) {
			t.Fatalf("expected SameRepo=true; docs=%+v remote=%+v", docs, remote)
		}
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameRepo(tc.docs, tc.remote); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
