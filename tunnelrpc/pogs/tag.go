package pogs

// Tag previously was a legacy tunnel capnp struct but was deprecated. To help reduce the amount of changes imposed
// by removing this simple struct, it was copied out of the capnp and provided here instead.
type Tag struct {
	Name  string
	Value string
}
