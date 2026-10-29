package strictjson

import "testing"

func TestDuplicateKeysAtEveryDepthAreRejected(t *testing.T) {
	for _, input := range []string{`{"name":"first","name":"second"}`, `{"name":"first","\u006eame":"second"}`, `{"nested":{"x":1,"x":2}}`, `{"nested":[{"x":1,"x":2}]}`, `{} {}`} {
		var out map[string]any
		if err := Decode([]byte(input), &out); err == nil {
			t.Fatalf("ambiguous JSON accepted: %s", input)
		}
	}
	var out struct {
		Name string `json:"name"`
	}
	if err := Decode([]byte(`{"name":"valid"}`), &out); err != nil || out.Name != "valid" {
		t.Fatal(err)
	}
	if err := Decode([]byte(`{"unknown":true}`), &out); err == nil {
		t.Fatal("unknown field accepted")
	}
}
