LRU Cache
---------

A `golang` implementation of last recently used cache data structure.

To install:

    go get github.com/cloudflare/golibs/lrucache

To test:

    cd $GOPATH/src/github.com/cloudflare/golibs/lrucache
    make test

For coverage:

    make cover

Basic benchmarks:

    $ make bench  # As tested on my two core i5
    [*] Scalability of cache/lrucache
    [ ] Operations in shared cache using one core
    BenchmarkConcurrentGetLRUCache       5000000               450 ns/op
    BenchmarkConcurrentSetLRUCache       2000000               821 ns/op
    BenchmarkConcurrentSetNXLRUCache     5000000               664 ns/op

    [*] Scalability of cache/multilru
    [ ] Operations in four caches using four cores
    BenchmarkConcurrentGetMultiLRU-4     5000000               475 ns/op
    BenchmarkConcurrentSetMultiLRU-4     2000000               809 ns/op
    BenchmarkConcurrentSetNXMultiLRU-4   5000000               643 ns/op

    [*] Capacity=4096 Keys=30000 KeySpace=15625
                vitess          LRUCache        MultiLRUCache-4
    create      1.709us         1.626374ms      343.54us
    Get (miss)  144.266083ms    132.470397ms    177.277193ms
    SetNX #1    338.637977ms    380.733302ms    411.709204ms
    Get (hit)   195.896066ms    173.252112ms    234.109494ms
    SetNX #2    349.785951ms    367.255624ms    419.129127ms
