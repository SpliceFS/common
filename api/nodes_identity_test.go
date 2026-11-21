package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SpliceFS/common/api"
	"github.com/SpliceFS/common/apitest"
	"github.com/SpliceFS/common/wire"
)

// TestNodesCarryIdentity lists nodes from an apitest node list: the node that has an id
// and keys returns all three, the node of an older version returns none and has no such
// names in its JSON at all.
func TestNodesCarryIdentity(t *testing.T) {
	t.Parallel()
	list := &apitest.NodeList{}
	full := wire.Node{
		Name: "east", URL: "https://east.example.com/api",
		ID: "0193b5d0-7c4e-7000-8000-000000000001", SignKey: "c2lnbg==", EncryptKey: "ZW5jcnlwdA==",
	}
	old := wire.Node{Name: "west", URL: "https://west.example.com/api"}
	list.Set(wire.Nodes{Cluster: "example", Nodes: []wire.Node{full, old}})
	auth := apitest.NewAuth(time.Now, time.Hour)
	auth.AddUser("alice", "secret", "*")
	token, _, err := auth.Login(context.Background(), "192.0.2.1", "alice", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	handler := api.NewHandler(api.Options{BasePath: "/api", Auth: auth, Nodes: list})

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/nodes", http.NoBody)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body)
	}
	var got struct {
		Nodes []map[string]string `json:"nodes"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %s: %v", recorder.Body, err)
	}
	want := []map[string]string{
		{
			"name": "east", "url": "https://east.example.com/api",
			"id": full.ID, "sign_key": full.SignKey, "encrypt_key": full.EncryptKey,
		},
		{"name": "west", "url": "https://west.example.com/api"},
	}
	if len(got.Nodes) != len(want) {
		t.Fatalf("nodes = %v, want %v", got.Nodes, want)
	}
	for i := range want {
		if !equalStrings(got.Nodes[i], want[i]) {
			t.Errorf("node %d = %v, want %v", i, got.Nodes[i], want[i])
		}
	}
}

// equalStrings reports whether a and b hold the same keys and values.
func equalStrings(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, ok := b[key]; !ok || other != value {
			return false
		}
	}
	return true
}
