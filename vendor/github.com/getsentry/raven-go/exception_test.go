package raven

import (
	"encoding/json"
	"errors"
	"testing"
)

var newExceptionTests = []struct {
	err error
	Exception
}{
	{errors.New("foobar"), Exception{Value: "foobar", Type: "*errors.errorString"}},
	{errors.New("bar: foobar"), Exception{Value: "foobar", Type: "*errors.errorString", Module: "bar"}},
}

func TestNewException(t *testing.T) {
	for _, test := range newExceptionTests {
		actual := NewException(test.err, nil)
		if actual.Value != test.Value {
			t.Errorf("incorrect Value: got %s, want %s", actual.Value, test.Value)
		}
		if actual.Type != test.Type {
			t.Errorf("incorrect Type: got %s, want %s", actual.Type, test.Type)
		}
		if actual.Module != test.Module {
			t.Errorf("incorrect Module: got %s, want %s", actual.Module, test.Module)
		}
	}
}

func TestNewException_JSON(t *testing.T) {
	expected := `{"value":"foobar","type":"*errors.errorString"}`
	e := NewException(errors.New("foobar"), nil)
	b, _ := json.Marshal(e)
	if string(b) != expected {
		t.Errorf("incorrect JSON: got %s, want %s", string(b), expected)
	}
}
