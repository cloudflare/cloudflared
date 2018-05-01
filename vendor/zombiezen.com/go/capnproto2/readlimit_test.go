package capnp

import "testing"

func TestReadLimiter_canRead(t *testing.T) {
	t.Parallel()
	type canReadCall struct {
		sz Size
		ok bool
	}
	tests := []struct {
		name  string
		init  uint64
		calls []canReadCall
	}{
		{
			name: "read a word with default limit",
			calls: []canReadCall{
				{8, true},
			},
		},
		{
			name: "reading a word from a high limit is okay",
			init: 128,
			calls: []canReadCall{
				{8, true},
			},
		},
		{
			name: "reading a byte after depleting the limit fails",
			init: 8,
			calls: []canReadCall{
				{8, true},
				{1, false},
			},
		},
		{
			name: "reading a byte after hitting the limit fails",
			init: 8,
			calls: []canReadCall{
				{8, true},
				{1, false},
			},
		},
		{
			name: "reading a byte after hitting the limit in multiple calls fails",
			init: 8,
			calls: []canReadCall{
				{6, true},
				{2, true},
				{1, false},
			},
		},
	}
	for _, test := range tests {
		m := &Message{TraverseLimit: test.init}
		for i, c := range test.calls {
			ok := m.ReadLimiter().canRead(c.sz)
			if ok != c.ok {
				// TODO(light): show previous calls
				t.Errorf("in %s, calls[%d] ok = %t; want %t", test.name, i, ok, c.ok)
			}
		}
	}
}

func TestReadLimiter_Reset(t *testing.T) {
	{
		m := &Message{TraverseLimit: 42}
		t.Log("   m := &Message{TraverseLimit: 42}")
		ok := m.ReadLimiter().canRead(42)
		t.Logf("   m.ReadLimiter().canRead(42) -> %t", ok)
		m.ReadLimiter().Reset(8)
		t.Log("   m.ReadLimiter().Reset(8)")
		if m.ReadLimiter().canRead(8) {
			t.Log("   m.ReadLimiter().canRead(8) -> true")
		} else {
			t.Error("!! m.ReadLimiter().canRead(8) -> false; want true")
		}
	}
	t.Log()
	{
		m := &Message{TraverseLimit: 42}
		t.Log("   m := &Message{TraverseLimit: 42}")
		ok := m.ReadLimiter().canRead(40)
		t.Logf("   m.ReadLimiter().canRead(40) -> %t", ok)
		m.ReadLimiter().Reset(8)
		t.Log("   m.ReadLimiter().Reset(8)")
		if m.ReadLimiter().canRead(9) {
			t.Error("!! m.ReadLimiter().canRead(9) -> true; want false")
		} else {
			t.Log("   m.ReadLimiter().canRead(9) -> false")
		}
	}
	t.Log()
	{
		m := new(Message)
		t.Log("   m := new(Message)")
		m.ReadLimiter().Reset(0)
		t.Log("   m.ReadLimiter().Reset(0)")
		if !m.ReadLimiter().canRead(0) {
			t.Error("!! m.ReadLimiter().canRead(0) -> false; want true")
		} else {
			t.Log("   m.ReadLimiter().canRead(0) -> true")
		}
	}
	t.Log()
	{
		m := new(Message)
		t.Log("   m := new(Message)")
		m.ReadLimiter().Reset(0)
		t.Log("   m.ReadLimiter().Reset(0)")
		if m.ReadLimiter().canRead(1) {
			t.Error("!! m.ReadLimiter().canRead(1) -> true; want false")
		} else {
			t.Log("   m.ReadLimiter().canRead(1) -> false")
		}
	}
}

func TestReadLimiter_Unread(t *testing.T) {
	{
		m := &Message{TraverseLimit: 42}
		t.Log("   m := &Message{TraverseLimit: 42}")
		ok := m.ReadLimiter().canRead(42)
		t.Logf("   m.ReadLimiter().canRead(42) -> %t", ok)
		m.ReadLimiter().Unread(8)
		t.Log("   m.ReadLimiter().Unread(8)")
		if m.ReadLimiter().canRead(8) {
			t.Log("   m.ReadLimiter().canRead(8) -> true")
		} else {
			t.Error("!! m.ReadLimiter().canRead(8) -> false; want true")
		}
	}
	t.Log()
	{
		m := &Message{TraverseLimit: 42}
		t.Log("   m := &Message{TraverseLimit: 42}")
		ok := m.ReadLimiter().canRead(40)
		t.Logf("   m.ReadLimiter().canRead(40) -> %t", ok)
		m.ReadLimiter().Unread(8)
		t.Log("   m.ReadLimiter().Unread(8)")
		if m.ReadLimiter().canRead(9) {
			t.Log("   m.ReadLimiter().canRead(9) -> true")
		} else {
			t.Error("!! m.ReadLimiter().canRead(9) -> false; want true")
		}
	}
}
