package hello

import (
	"testing"
)

func TestCreateTLSListenerHostAndPortSuccess(t *testing.T) {
	listener, err := CreateTLSListener("localhost:1234")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}

func TestCreateTLSListenerOnlyHostSuccess(t *testing.T) {
	listener, err := CreateTLSListener("localhost:")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}

func TestCreateTLSListenerOnlyPortSuccess(t *testing.T) {
	listener, err := CreateTLSListener("localhost:8888")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}
