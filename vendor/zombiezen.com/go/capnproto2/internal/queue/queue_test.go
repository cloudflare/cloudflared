package queue

import "testing"

func TestNew(t *testing.T) {
	qi := make(ints, 5)

	q := New(qi, 0)

	if n := q.Len(); n != 0 {
		t.Errorf("New(qi, 0).Len() = %d; want 0", n)
	}
}

func TestPrepush(t *testing.T) {
	qi := make(ints, 5)
	qi[0] = 42

	q := New(qi, 1)

	if n := q.Len(); n != 1 {
		t.Fatalf("New(qi, 1).Len() = %d; want 1", n)
	}
	if i := q.Front(); i != 0 {
		t.Errorf("q.Front() = %d; want 0", i)
	}
}

func TestPush(t *testing.T) {
	qi := make(ints, 5)
	q := New(qi, 0)

	i := q.Push()
	if i == -1 {
		t.Error("q.Push() returned -1")
	}
	qi[i] = 42

	if n := q.Len(); n != 1 {
		t.Errorf("q.Len() after push = %d; want 1", n)
	}
	if front := q.Front(); front != i {
		t.Errorf("q.Front() after push = %d; want %d", front, i)
	}
}

func TestPushFull(t *testing.T) {
	qi := make(ints, 5)
	q := New(qi, 0)
	var ok [6]bool

	push := func(n int, val int) {
		i := q.Push()
		if i == -1 {
			return
		}
		ok[n] = true
		qi[i] = val
	}
	push(0, 10)
	push(1, 11)
	push(2, 12)
	push(3, 13)
	push(4, 14)
	push(5, 15)

	for i := 0; i < 5; i++ {
		if !ok[i] {
			t.Errorf("q.Push() #%d returned -1", i)
		}
	}
	if ok[5] {
		t.Error("q.Push() #5 returned true")
	}
	if n := q.Len(); n != 5 {
		t.Errorf("q.Len() after full = %d; want 5", n)
	}
}

func TestPop(t *testing.T) {
	qi := make(ints, 5)
	q := New(qi, 0)
	qi[q.Push()] = 1
	qi[q.Push()] = 2
	qi[q.Push()] = 3

	outs := make([]int, 3)
	for n := range outs {
		i := q.Front()
		if i == -1 {
			t.Fatalf("before q.Pop() #%d, Front == -1", n)
		}
		outs[n] = qi[i]
		if !q.Pop() {
			t.Fatalf("q.Pop() #%d = false", n)
		}
	}

	if n := q.Len(); n != 0 {
		t.Errorf("q.Len() after pops = %d; want 0", n)
	}
	if outs[0] != 1 {
		t.Errorf("pop #0 = %d; want 1", outs[0])
	}
	if outs[1] != 2 {
		t.Errorf("pop #1 = %d; want 2", outs[1])
	}
	if outs[2] != 3 {
		t.Errorf("pop #2 = %d; want 3", outs[2])
	}
	for i := range qi {
		if qi[i] != 0 {
			t.Errorf("qi[%d] = %d; want 0 (not cleared)", i, qi[i])
		}
	}
}

func TestWrap(t *testing.T) {
	qi := make(ints, 5)
	q := New(qi, 0)

	qi[q.Push()] = 10
	qi[q.Push()] = 11
	qi[q.Push()] = 12
	q.Pop()
	q.Pop()
	qi[q.Push()] = 13
	qi[q.Push()] = 14
	qi[q.Push()] = 15
	qi[q.Push()] = 16

	if n := q.Len(); n != 5 {
		t.Errorf("q.Len() = %d; want 5", n)
	}
	for i := 12; q.Len() > 0; i++ {
		if x := qi[q.Front()]; x != i {
			t.Errorf("qi[q.Front()] = %d; want %d", x, i)
		}
		if !q.Pop() {
			t.Error("q.Pop() returned false")
			break
		}
	}
}

type ints []int

func (is ints) Len() int {
	return len(is)
}

func (is ints) Clear(i int) {
	is[i] = 0
}
