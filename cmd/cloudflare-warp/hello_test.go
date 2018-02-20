package main

import (
	"testing"
)

func TestCreateListenerHostAndPortSuccess(t *testing.T) {
	listener, err := createListener("localhost:1234")
	if err != nil {
		t.Fatal(err)
	}
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}

func TestCreateListenerOnlyHostSuccess(t *testing.T) {
	listener, err := createListener("localhost:")
	if err != nil {
		t.Fatal(err)
	}
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}

func TestCreateListenerOnlyPortSuccess(t *testing.T) {
	listener, err := createListener(":8888")
	if err != nil {
		t.Fatal(err)
	}
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}
