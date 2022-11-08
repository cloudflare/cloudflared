package packet

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type mockFunnelUniPipe struct {
	uniPipe chan RawPacket
}

func (mfui *mockFunnelUniPipe) SendPacket(dst netip.Addr, pk RawPacket) error {
	mfui.uniPipe <- pk
	return nil
}

func (mfui *mockFunnelUniPipe) Close() error {
	return nil
}

func TestFunnelRegistration(t *testing.T) {
	id := testFunnelID{"id1"}
	funnelErr := fmt.Errorf("expected error")
	newFunnelFuncErr := func() (Funnel, error) { return nil, funnelErr }
	newFunnelFuncUncalled := func() (Funnel, error) {
		require.FailNow(t, "a new funnel should not be created")
		panic("unreached")
	}
	funnel1, newFunnelFunc1 := newFunnelAndFunc("funnel1")
	funnel2, newFunnelFunc2 := newFunnelAndFunc("funnel2")

	ft := NewFunnelTracker()
	// Register funnel1
	funnel, new, err := ft.GetOrRegister(id, shouldReplaceFalse, newFunnelFunc1)
	require.NoError(t, err)
	require.True(t, new)
	require.Equal(t, funnel1, funnel)
	// Register funnel, no replace
	funnel, new, err = ft.GetOrRegister(id, shouldReplaceFalse, newFunnelFuncUncalled)
	require.NoError(t, err)
	require.False(t, new)
	require.Equal(t, funnel1, funnel)
	// Register funnel2, replace
	funnel, new, err = ft.GetOrRegister(id, shouldReplaceTrue, newFunnelFunc2)
	require.NoError(t, err)
	require.True(t, new)
	require.Equal(t, funnel2, funnel)
	require.True(t, funnel1.closed)
	// Register funnel error, replace
	funnel, new, err = ft.GetOrRegister(id, shouldReplaceTrue, newFunnelFuncErr)
	require.ErrorIs(t, err, funnelErr)
	require.False(t, new)
	require.Nil(t, funnel)
	require.True(t, funnel2.closed)
}

func TestFunnelUnregister(t *testing.T) {
	id := testFunnelID{"id1"}
	funnel1, newFunnelFunc1 := newFunnelAndFunc("funnel1")
	funnel2, newFunnelFunc2 := newFunnelAndFunc("funnel2")
	funnel3, newFunnelFunc3 := newFunnelAndFunc("funnel3")

	ft := NewFunnelTracker()
	// Register & unregister
	_, _, err := ft.GetOrRegister(id, shouldReplaceFalse, newFunnelFunc1)
	require.NoError(t, err)
	require.True(t, ft.Unregister(id, funnel1))
	require.True(t, funnel1.closed)
	require.True(t, ft.Unregister(id, funnel1))
	// Register, replace, and unregister
	_, _, err = ft.GetOrRegister(id, shouldReplaceFalse, newFunnelFunc2)
	require.NoError(t, err)
	_, _, err = ft.GetOrRegister(id, shouldReplaceTrue, newFunnelFunc3)
	require.NoError(t, err)
	require.True(t, funnel2.closed)
	require.False(t, ft.Unregister(id, funnel2))
	require.True(t, ft.Unregister(id, funnel3))
	require.True(t, funnel3.closed)
}

func shouldReplaceFalse(_ Funnel) bool {
	return false
}

func shouldReplaceTrue(_ Funnel) bool {
	return true
}

func newFunnelAndFunc(id string) (*testFunnel, func() (Funnel, error)) {
	funnel := newTestFunnel(id)
	funnelFunc := func() (Funnel, error) {
		return funnel, nil
	}
	return funnel, funnelFunc
}

type testFunnelID struct {
	id string
}

func (t testFunnelID) Type() string {
	return "testFunnelID"
}

func (t testFunnelID) String() string {
	return t.id
}

type testFunnel struct {
	id     string
	closed bool
}

func newTestFunnel(id string) *testFunnel {
	return &testFunnel{
		id,
		false,
	}
}

func (tf *testFunnel) Close() error {
	tf.closed = true
	return nil
}

func (tf *testFunnel) Equal(other Funnel) bool {
	return tf.id == other.(*testFunnel).id
}

func (tf *testFunnel) LastActive() time.Time {
	return time.Now()
}

func (tf *testFunnel) UpdateLastActive() {}
