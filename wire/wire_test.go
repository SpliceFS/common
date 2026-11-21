package wire

import (
	"encoding/json"
	"testing"

	"go.yaml.in/yaml/v3"
)

// nodeCase pairs a node with its two encodings.
type nodeCase struct {
	name string
	node Node
	json string
	yaml string
}

// nodeCases returns the cases. The identity fields stay out of both encodings when they
// are empty, so a node that runs an older version looks the same as before.
func nodeCases() []nodeCase {
	return []nodeCase{
		{
			name: "with identity",
			node: Node{
				Name: "east", URL: "https://east.example.com/api",
				ID: "0193b5d0-7c4e-7000-8000-000000000001", SignKey: "c2lnbg==", EncryptKey: "ZW5jcnlwdA==",
			},
			json: `{"name":"east","url":"https://east.example.com/api",` +
				`"id":"0193b5d0-7c4e-7000-8000-000000000001","sign_key":"c2lnbg==","encrypt_key":"ZW5jcnlwdA=="}`,
			yaml: "name: east\nurl: https://east.example.com/api\n" +
				"id: 0193b5d0-7c4e-7000-8000-000000000001\nsign_key: c2lnbg==\nencrypt_key: ZW5jcnlwdA==\n",
		},
		{
			name: "without identity",
			node: Node{Name: "west", URL: "https://west.example.com/api"},
			json: `{"name":"west","url":"https://west.example.com/api"}`,
			yaml: "name: west\nurl: https://west.example.com/api\n",
		},
	}
}

func TestNodeJSON(t *testing.T) {
	t.Parallel()
	for _, tt := range nodeCases() {
		got, err := json.Marshal(tt.node)
		if err != nil || string(got) != tt.json {
			t.Errorf("%s: Marshal = %s, %v; want %s", tt.name, got, err, tt.json)
		}
		var back Node
		if err := json.Unmarshal([]byte(tt.json), &back); err != nil || back != tt.node {
			t.Errorf("%s: Unmarshal = %+v, %v; want %+v", tt.name, back, err, tt.node)
		}
	}
}

func TestNodeYAML(t *testing.T) {
	t.Parallel()
	for _, tt := range nodeCases() {
		got, err := yaml.Marshal(tt.node)
		if err != nil || string(got) != tt.yaml {
			t.Errorf("%s: Marshal = %q, %v; want %q", tt.name, got, err, tt.yaml)
		}
		var back Node
		if err := yaml.Unmarshal([]byte(tt.yaml), &back); err != nil || back != tt.node {
			t.Errorf("%s: Unmarshal = %+v, %v; want %+v", tt.name, back, err, tt.node)
		}
	}
}
