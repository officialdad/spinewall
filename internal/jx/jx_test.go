package jx

import (
	"encoding/json"
	"testing"
)

func TestDecodeNumbersStayNumbers(t *testing.T) {
	v, err := Decode([]byte(`{"a": 1, "b": 1.5, "c": 1e3}`))
	if err != nil {
		t.Fatal(err)
	}
	obj := v.(map[string]any)
	for _, key := range []string{"a", "b", "c"} {
		if _, ok := obj[key].(json.Number); !ok {
			t.Fatalf("%s is %T", key, obj[key])
		}
	}
}

func TestDecodeRejectsTrailing(t *testing.T) {
	if _, err := Decode([]byte(`{"a":1} {"b":2}`)); err == nil {
		t.Fatalf("trailing value accepted")
	}
}

func TestDecodeNaNAndInfinity(t *testing.T) {
	v, err := Decode([]byte(`{"scale": NaN, "x": Infinity}`))
	if err != nil {
		t.Fatalf("NaN rejected: %v", err)
	}
	obj := v.(map[string]any)
	if obj["scale"] != nil {
		t.Fatalf("NaN became %v", obj["scale"])
	}
	if n, ok := obj["x"].(json.Number); !ok || n.String() != "1e308" {
		t.Fatalf("Infinity became %v", obj["x"])
	}
}

func TestDecodeLeavesStringsAlone(t *testing.T) {
	v, err := Decode([]byte(`{"name": "NaN Infinity"}`))
	if err != nil {
		t.Fatal(err)
	}
	if v.(map[string]any)["name"] != "NaN Infinity" {
		t.Fatalf("string changed: %v", v)
	}
}
