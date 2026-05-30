// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package rebac_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/keese-ai/keese/internal/rebac"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cfg     rebac.Config
		wantErr string
	}{
		{
			name: "all fields set",
			cfg: rebac.Config{
				APIURL:               "http://localhost:8080",
				StoreID:              "01HX0",
				AuthorizationModelID: "01HX1",
			},
		},
		{
			name:    "empty APIURL",
			cfg:     rebac.Config{StoreID: "s", AuthorizationModelID: "m"},
			wantErr: "OPENFGA_API_URL",
		},
		{
			name:    "empty StoreID",
			cfg:     rebac.Config{APIURL: "http://x", AuthorizationModelID: "m"},
			wantErr: "OPENFGA_STORE_ID",
		},
		{
			name:    "empty AuthorizationModelID",
			cfg:     rebac.Config{APIURL: "http://x", StoreID: "s"},
			wantErr: "OPENFGA_AUTHORIZATION_MODEL_ID",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" && err != nil {
				t.Fatalf("Validate: got %v, want nil", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("Validate: got %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

// fakeFGAServer is a minimal stub of the OpenFGA HTTP API for testing.
// It implements only the endpoints the rebac.Client uses: Write, Read.
type fakeFGAServer struct {
	t  *testing.T
	mu sync.Mutex

	// tuples is the in-memory store keyed by (object,relation,user).
	tuples map[string]struct{}

	// behaviour overrides
	writeAlreadyExists bool
	deleteNotFound     bool
	writeStatus        int
	writeBodySnippet   string
}

func (s *fakeFGAServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/write"):
			s.handleWrite(w, r)
		case strings.HasSuffix(r.URL.Path, "/read"):
			s.handleRead(w, r)
		default:
			http.Error(w, "unsupported: "+r.URL.Path, http.StatusNotFound)
		}
	})
	return mux
}

func (s *fakeFGAServer) handleWrite(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()
	var req struct {
		Writes *struct {
			TupleKeys []tupleKey `json:"tuple_keys"`
		} `json:"writes,omitempty"`
		Deletes *struct {
			TupleKeys []tupleKey `json:"tuple_keys"`
		} `json:"deletes,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// Custom error injection
	if s.writeStatus != 0 {
		w.WriteHeader(s.writeStatus)
		_, _ = w.Write([]byte(s.writeBodySnippet))
		return
	}
	if req.Writes != nil {
		for _, k := range req.Writes.TupleKeys {
			id := k.id()
			if s.writeAlreadyExists {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":"write_failed_due_to_invalid_input","message":"tuple already exists"}`))
				return
			}
			s.tuples[id] = struct{}{}
		}
	}
	if req.Deletes != nil {
		for _, k := range req.Deletes.TupleKeys {
			id := k.id()
			if s.deleteNotFound {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":"write_failed_due_to_invalid_input","message":"cannot delete a tuple which does not exist"}`))
				return
			}
			delete(s.tuples, id)
		}
	}
	_, _ = w.Write([]byte(`{}`))
}

func (s *fakeFGAServer) handleRead(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()
	var req struct {
		TupleKey *tupleKey `json:"tuple_key,omitempty"`
	}
	_ = json.Unmarshal(body, &req)
	type respTuple struct {
		Key tupleKey `json:"key"`
	}
	w.Header().Set("Content-Type", "application/json")
	out := struct {
		Tuples            []respTuple `json:"tuples"`
		ContinuationToken string      `json:"continuation_token"`
	}{}
	for k := range s.tuples {
		parts := strings.SplitN(k, "|", 3)
		t := tupleKey{Object: parts[0], Relation: parts[1], User: parts[2]}
		if req.TupleKey != nil {
			if req.TupleKey.Object != "" && req.TupleKey.Object != t.Object {
				continue
			}
			if req.TupleKey.Relation != "" && req.TupleKey.Relation != t.Relation {
				continue
			}
			if req.TupleKey.User != "" && req.TupleKey.User != t.User {
				continue
			}
		}
		out.Tuples = append(out.Tuples, respTuple{Key: t})
	}
	_ = json.NewEncoder(w).Encode(out)
}

type tupleKey struct {
	Object   string `json:"object"`
	Relation string `json:"relation"`
	User     string `json:"user"`
}

func (k tupleKey) id() string { return k.Object + "|" + k.Relation + "|" + k.User }

func newFakeServer(t *testing.T) (*fakeFGAServer, *httptest.Server) {
	t.Helper()
	s := &fakeFGAServer{t: t, tuples: map[string]struct{}{}}
	hs := httptest.NewServer(s.handler())
	t.Cleanup(hs.Close)
	return s, hs
}

func newClient(t *testing.T, hs *httptest.Server) *rebac.Client {
	t.Helper()
	// SDK validates ULID format (26-char Crockford Base32) on both IDs.
	c, err := rebac.New(rebac.Config{
		APIURL:               hs.URL,
		StoreID:              "01HX0ABCDEFGHJKMNPQRSTVWXY",
		AuthorizationModelID: "01HX0BCDEFGHJKMNPQRSTVWXYZ",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestWriteIdempotent(t *testing.T) {
	t.Parallel()
	srv, hs := newFakeServer(t)
	c := newClient(t, hs)

	// First write succeeds.
	if err := c.Write(context.Background(), "workspace:my-ws", "owner", "tenant:alpha"); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if _, ok := srv.tuples["workspace:my-ws|owner|tenant:alpha"]; !ok {
		t.Fatal("tuple missing after Write")
	}

	// Configure the server to return "already exists" on next write.
	srv.mu.Lock()
	srv.writeAlreadyExists = true
	srv.mu.Unlock()

	// Second write should swallow the error (idempotent semantics).
	if err := c.Write(context.Background(), "workspace:my-ws", "owner", "tenant:alpha"); err != nil {
		t.Fatalf("idempotent Write: got %v, want nil", err)
	}
}

func TestDeleteIdempotent(t *testing.T) {
	t.Parallel()
	srv, hs := newFakeServer(t)
	c := newClient(t, hs)

	// Write then delete succeeds.
	if err := c.Write(context.Background(), "workspace:ws-a", "viewer", "user:alice"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := c.Delete(context.Background(), "workspace:ws-a", "viewer", "user:alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := srv.tuples["workspace:ws-a|viewer|user:alice"]; ok {
		t.Fatal("tuple present after Delete")
	}

	// Configure the server to return "not found" on next delete.
	srv.mu.Lock()
	srv.deleteNotFound = true
	srv.mu.Unlock()

	// Second delete should swallow the error.
	if err := c.Delete(context.Background(), "workspace:ws-a", "viewer", "user:alice"); err != nil {
		t.Fatalf("idempotent Delete: got %v, want nil", err)
	}
}

func TestWritePropagatesUnexpectedError(t *testing.T) {
	t.Parallel()
	srv, hs := newFakeServer(t)
	c := newClient(t, hs)

	srv.mu.Lock()
	srv.writeStatus = http.StatusInternalServerError
	srv.writeBodySnippet = `{"code":"internal_error","message":"boom"}`
	srv.mu.Unlock()

	err := c.Write(context.Background(), "workspace:ws-b", "owner", "tenant:beta")
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	if !strings.Contains(err.Error(), "workspace:ws-b") {
		t.Fatalf("error should mention tuple, got %v", err)
	}
}

func TestReadFiltersByObject(t *testing.T) {
	t.Parallel()
	_, hs := newFakeServer(t)
	c := newClient(t, hs)
	ctx := context.Background()

	// Seed several tuples across two extensions.
	mustWrite(t, c, "extension:foo", "owner", "tenant:alpha")
	mustWrite(t, c, "extension:foo", "enabled_in", "workspace:ws-a")
	mustWrite(t, c, "extension:foo", "enabled_in", "workspace:ws-b")
	mustWrite(t, c, "extension:bar", "owner", "tenant:beta")

	got, err := c.Read(ctx, "extension:foo", "", "")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Read object=foo: got %d tuples, want 3 (%v)", len(got), got)
	}

	gotEnabled, err := c.Read(ctx, "extension:foo", "enabled_in", "")
	if err != nil {
		t.Fatalf("Read filtered: %v", err)
	}
	if len(gotEnabled) != 2 {
		t.Fatalf("Read object=foo,relation=enabled_in: got %d, want 2", len(gotEnabled))
	}
}

func mustWrite(t *testing.T, c *rebac.Client, object, relation, user string) {
	t.Helper()
	if err := c.Write(context.Background(), object, relation, user); err != nil {
		t.Fatalf("seed Write %s#%s@%s: %v", object, relation, user, err)
	}
}
