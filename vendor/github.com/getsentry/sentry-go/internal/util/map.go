package util

import "sync"

type SyncMap[K comparable, V any] struct {
	m sync.Map
}

func (s *SyncMap[K, V]) Store(key K, value V) {
	s.m.Store(key, value)
}

func (s *SyncMap[K, V]) CompareAndDelete(key K, value V) {
	s.m.CompareAndDelete(key, value)
}

func (s *SyncMap[K, V]) Load(key K) (V, bool) {
	v, ok := s.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true
}

func (s *SyncMap[K, V]) Delete(key K) {
	s.m.Delete(key)
}

func (s *SyncMap[K, V]) LoadOrStore(key K, value V) (V, bool) {
	actual, loaded := s.m.LoadOrStore(key, value)
	return actual.(V), loaded
}

func (s *SyncMap[K, V]) Clear() {
	s.m.Clear()
}

func (s *SyncMap[K, V]) Range(f func(key K, value V) bool) {
	s.m.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}
