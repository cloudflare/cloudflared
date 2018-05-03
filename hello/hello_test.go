package hello

import (
	"testing"
)

func TestCreateTLSListenerHostAndPortSuccess(t *testing.T) {
	listener, err := CreateTLSListener("localhost:1234")
	defer listener.Close()
	if err != nil {
		t.Fatal(err)
	}
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}

func TestCreateTLSListenerOnlyHostSuccess(t *testing.T) {
	listener, err := CreateTLSListener("localhost:")
	defer listener.Close()
	if err != nil {
		t.Fatal(err)
	}
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}

func TestCreateTLSListenerOnlyPortSuccess(t *testing.T) {
	listener, err := CreateTLSListener(":8888")
	defer listener.Close()
	if err != nil {
		t.Fatal(err)
	}
	if listener.Addr().String() == "" {
		t.Fatal("Fail to find available port")
	}
}
