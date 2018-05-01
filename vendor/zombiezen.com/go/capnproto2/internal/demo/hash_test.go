package demo_test

import (
	"crypto/sha1"
	"fmt"
	"hash"
	"net"

	"golang.org/x/net/context"
	"zombiezen.com/go/capnproto2/internal/demo/hashes"
	"zombiezen.com/go/capnproto2/rpc"
)

// hashFactory is a local implementation of HashFactory.
type hashFactory struct{}

func (hf hashFactory) NewSha1(call hashes.HashFactory_newSha1) error {
	// Create a new locally implemented Hash capability.
	hs := hashes.Hash_ServerToClient(hashServer{sha1.New()})
	// Notice that methods can return other interfaces.
	return call.Results.SetHash(hs)
}

// hashServer is a local implementation of Hash.
type hashServer struct {
	h hash.Hash
}

func (hs hashServer) Write(call hashes.Hash_write) error {
	data, err := call.Params.Data()
	if err != nil {
		return err
	}
	_, err = hs.h.Write(data)
	if err != nil {
		return err
	}
	return nil
}

func (hs hashServer) Sum(call hashes.Hash_sum) error {
	s := hs.h.Sum(nil)
	return call.Results.SetHash(s)
}

func server(c net.Conn) error {
	// Create a new locally implemented HashFactory.
	main := hashes.HashFactory_ServerToClient(hashFactory{})
	// Listen for calls, using the HashFactory as the bootstrap interface.
	conn := rpc.NewConn(rpc.StreamTransport(c), rpc.MainInterface(main.Client))
	// Wait for connection to abort.
	err := conn.Wait()
	return err
}

func client(ctx context.Context, c net.Conn) error {
	// Create a connection that we can use to get the HashFactory.
	conn := rpc.NewConn(rpc.StreamTransport(c))
	defer conn.Close()
	// Get the "bootstrap" interface.  This is the capability set with
	// rpc.MainInterface on the remote side.
	hf := hashes.HashFactory{Client: conn.Bootstrap(ctx)}

	// Now we can call methods on hf, and they will be sent over c.
	s := hf.NewSha1(ctx, nil).Hash()
	// s refers to a remote Hash.  Method calls are delivered in order.
	s.Write(ctx, func(p hashes.Hash_write_Params) error {
		err := p.SetData([]byte("Hello, "))
		return err
	})
	s.Write(ctx, func(p hashes.Hash_write_Params) error {
		err := p.SetData([]byte("World!"))
		return err
	})
	result, err := s.Sum(ctx, nil).Struct()
	if err != nil {
		return err
	}
	sha1Val, err := result.Hash()
	if err != nil {
		return err
	}
	fmt.Printf("sha1: %x\n", sha1Val)
	return nil
}

func Example_hash() {
	c1, c2 := net.Pipe()
	go server(c1)
	client(context.Background(), c2)
	// Output:
	// sha1: 0a0a9f2a6772942557ab5355d76af442f8f65e01
}
