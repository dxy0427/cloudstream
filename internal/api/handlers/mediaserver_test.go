package handlers

import "testing"

func TestMediaServerCreateBooleanDefaults(t *testing.T) {
	if !boolOrDefault(nil, true) {
		t.Fatal("omitted true-default boolean became false")
	}
	if boolOrDefault(nil, false) {
		t.Fatal("omitted false-default boolean became true")
	}
	value := false
	if boolOrDefault(&value, true) {
		t.Fatal("explicit false was not preserved")
	}
}
