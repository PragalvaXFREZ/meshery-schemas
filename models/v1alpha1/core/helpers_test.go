package core

import "testing"

func TestMapObjectGormDataType(t *testing.T) {
	var value MapObject
	if got := value.GormDataType(); got != "text" {
		t.Fatalf("unexpected GORM data type: got %q, want %q", got, "text")
	}
}

func TestMapObjectValueMarshalsNilAsJSONNull(t *testing.T) {
	var value MapObject
	serialized, err := value.Value()
	if err != nil {
		t.Fatalf("unexpected error marshaling nil map object: %v", err)
	}

	serializedString, ok := serialized.(string)
	if !ok {
		t.Fatalf("unexpected serialized value type: got %T, want string", serialized)
	}

	if serializedString != "null" {
		t.Fatalf("unexpected serialized nil map object: got %q, want %q", serializedString, "null")
	}
}

func TestMapObjectScanStringRoundTrip(t *testing.T) {
	var value MapObject
	if err := value.Scan(`{"source":"test","mode":"gorm"}`); err != nil {
		t.Fatalf("unexpected error scanning map object: %v", err)
	}

	if got := value["source"]; got != "test" {
		t.Fatalf("unexpected source value: got %q, want %q", got, "test")
	}

	if got := value["mode"]; got != "gorm" {
		t.Fatalf("unexpected mode value: got %q, want %q", got, "gorm")
	}
}
