// Copyright (c) 2013 CloudFlare, Inc.

// Extensions to "container/list" that allowing reuse of Elements.

package lrucache

func (l *list) PushElementFront(e *element) *element {
	return l.insert(e, &l.root)
}

func (l *list) PushElementBack(e *element) *element {
	return l.insert(e, l.root.prev)
}

func (l *list) PopElementFront() *element {
	el := l.Front()
	l.Remove(el)
	return el
}

func (l *list) PopFront() interface{} {
	el := l.Front()
	l.Remove(el)
	return el.Value
}
