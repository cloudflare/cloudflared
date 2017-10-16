package main

import (
	"testing"
)

const testPort = "8080"

func TestNewHelloWorldServer(t *testing.T) {
	if NewHelloWorldServer() == nil {
		t.Fatal("NewHelloWorldServer returned nil")
	}
}

func TestFindAvailablePort(t *testing.T) {
	listener, err := findAvailablePort()
	if err != nil {
		t.Fatal("Fail to find available port")
	}
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}
