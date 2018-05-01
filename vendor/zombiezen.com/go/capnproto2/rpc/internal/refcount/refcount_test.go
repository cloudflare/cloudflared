package refcount

import (
	"testing"

	"zombiezen.com/go/capnproto2"
)

func TestSingleRefCloses(t *testing.T) {
	c := new(fakeClient)

	_, ref := New(c)
	err := ref.Close()

	if err != nil {
		t.Errorf("ref.Close(): %v", err)
	}
	if c.closed != 1 {
		t.Errorf("client Close() called %d times; want 1 time", c.closed)
	}
}

func TestCloseRefMultipleDecrefsOnce(t *testing.T) {
	c := new(fakeClient)

	rc, ref1 := New(c)
	ref2 := rc.Ref()
	err1 := ref1.Close()
	err2 := ref1.Close()
	_ = ref2

	if err1 != nil {
		t.Errorf("ref.Close() #1: %v", err1)
	}
	if err2 != errClosed {
		t.Errorf("ref.Close() #2: %v; want %v", err2, errClosed)
	}
	if c.closed != 0 {
		t.Errorf("client Close() called %d times; want 0 times", c.closed)
	}
}

func TestClosingOneOfManyRefsDoesntClose(t *testing.T) {
	c := new(fakeClient)

	rc, ref1 := New(c)
	ref2 := rc.Ref()
	err := ref1.Close()
	_ = ref2

	if err != nil {
		t.Errorf("ref1.Close(): %v", err)
	}
	if c.closed != 0 {
		t.Errorf("client Close() called %d times; want 0 times", c.closed)
	}
}

func TestClosingAllRefsCloses(t *testing.T) {
	c := new(fakeClient)

	rc, ref1 := New(c)
	ref2 := rc.Ref()
	err1 := ref1.Close()
	err2 := ref2.Close()

	if err1 != nil {
		t.Errorf("ref1.Close(): %v", err1)
	}
	if err2 != nil {
		t.Errorf("ref2.Close(): %v", err2)
	}
	if c.closed != 1 {
		t.Errorf("client Close() called %d times; want 1 times", c.closed)
	}
}

type fakeClient struct {
	closed int
}

func (c *fakeClient) Call(cl *capnp.Call) capnp.Answer {
	panic("not implemented")
}

func (c *fakeClient) Close() error {
	c.closed++
	return nil
}
