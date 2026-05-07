package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"ftg": main,
	})
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			// Per-script stub for the GitHub Releases API. Scripts that don't
			// touch the update check still get a server URL via env var; the
			// updatecheck gate keeps the server idle for those.
			tag := "v9.9.9"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag})
			}))
			env.Defer(srv.Close)
			env.Setenv("FIND_THE_GAPS_UPDATE_STUB_URL", srv.URL)
			return nil
		},
	})
}
