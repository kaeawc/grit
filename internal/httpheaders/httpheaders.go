// Package httpheaders centralises the static + env-bound HTTP header
// pattern shared by the grit downloader, publisher, and gradlecache
// fetcher. A Set captures headers declared at construction time and
// applies them onto an http.Header at request time, resolving any
// env-bound values lazily so credentials can rotate without rebuilding
// the owning struct.
package httpheaders

import (
	"net/http"
	"os"
)

// Set is a list of static and env-bound HTTP headers. The zero value
// is empty and safe to apply.
type Set struct {
	static []pair
	envs   []envBinding
}

type pair struct {
	header string
	value  string
}

type envBinding struct {
	header string
	envVar string
}

// AddStatic records header=value. Empty header is ignored. Empty value
// is kept in the set but skipped at Apply time so placeholder bindings
// don't surface blank request headers.
func (s *Set) AddStatic(header, value string) {
	if header == "" {
		return
	}
	s.static = append(s.static, pair{header: header, value: value})
}

// AddStaticMap records every entry in m via AddStatic. A nil or empty
// map is a no-op.
func (s *Set) AddStaticMap(m map[string]string) {
	for k, v := range m {
		s.AddStatic(k, v)
	}
}

// AddEnv binds header to the value of envVar, resolved at Apply time.
// Empty header or envVar is ignored.
func (s *Set) AddEnv(header, envVar string) {
	if header == "" || envVar == "" {
		return
	}
	s.envs = append(s.envs, envBinding{header: header, envVar: envVar})
}

// Apply writes the set onto h. Static headers go first, then env
// headers (so env bindings can override a static placeholder for the
// same key). Empty values are skipped — both for static entries and
// for env vars that are unset or blank.
func (s *Set) Apply(h http.Header) {
	for _, p := range s.static {
		if p.value != "" {
			h.Set(p.header, p.value)
		}
	}
	for _, b := range s.envs {
		if v, ok := os.LookupEnv(b.envVar); ok && v != "" {
			h.Set(b.header, v)
		}
	}
}
