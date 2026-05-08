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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameRepo(tc.docs, tc.remote); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
