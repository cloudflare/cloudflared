// Copyright (c) 2013 CloudFlare, Inc.

// Package lrucache implements a last recently used cache data structure.
//
// This code tries to avoid dynamic memory allocations - all required
// memory is allocated on creation.  Access to the data structure is
// O(1). Modification O(log(n)) if expiry is used, O(1)
// otherwise.
//
// This package exports three things:
//  LRUCache: is the main implementation. It supports multithreading by
//      using guarding mutex lock.
//
//  MultiLRUCache: is a sharded implementation. It supports the same
//      API as LRUCache and uses it internally, but is not limited to
//      a single CPU as every shard is separately locked. Use this
//      data structure instead of LRUCache if you have have lock
//      contention issues.
//
//  Cache interface: Both implementations fulfill it.
package lrucache

import (
	"time"
)

// Cache interface is fulfilled by the LRUCache and MultiLRUCache
// implementations.
type Cache interface {
	// Methods not needing to know current time.
	//
	// Get a key from the cache, possibly stale. Update its LRU
	// score.
	Get(key string) (value interface{}, ok bool)
	// Get a key from the cache, possibly stale. Don't modify its LRU score. O(1)
	GetQuiet(key string) (value interface{}, ok bool)
	// Get and remove a key from the cache.
	Del(key string) (value interface{}, ok bool)
	// Evict all items from the cache.
	Clear() int
	// Number of entries used in the LRU
	Len() int
	// Get the total capacity of the LRU
	Capacity() int

	// Methods use time.Now() when neccessary to determine expiry.
	//
	// Add an item to the cache overwriting existing one if it
	// exists.
	Set(key string, value interface{}, expire time.Time)
	// Get a key from the cache, make sure it's not stale. Update
	// its LRU score.
	GetNotStale(key string) (value interface{}, ok bool)
	// Evict all the expired items.
	Expire() int

	// Methods allowing to explicitly specify time used to
	// determine if items are expired.
	//
	// Add an item to the cache overwriting existing one if it
	// exists. Allows specifing current time required to expire an
	// item when no more slots are used.
	SetNow(key string, value interface{}, expire time.Time, now time.Time)
	// Get a key from the cache, make sure it's not stale. Update
	// its LRU score.
	GetNotStaleNow(key string, now time.Time) (value interface{}, ok bool)
	// Evict items that expire before Now.
	ExpireNow(now time.Time) int
}
