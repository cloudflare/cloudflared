package attribute

type Builder struct {
	Key   string
	Value Value
}

// String returns a Builder for a string value.
func String(key, value string) Builder {
	return Builder{key, StringValue(value)}
}

// Int64 returns a Builder for an int64.
func Int64(key string, value int64) Builder {
	return Builder{key, Int64Value(value)}
}

// Int returns a Builder for an int64.
func Int(key string, value int) Builder {
	return Builder{key, IntValue(value)}
}

// Float64 returns a Builder for a float64.
func Float64(key string, v float64) Builder {
	return Builder{key, Float64Value(v)}
}

// Bool returns a Builder for a boolean.
func Bool(key string, v bool) Builder {
	return Builder{key, BoolValue(v)}
}

// Valid checks for valid key and type.
func (b *Builder) Valid() bool {
	return len(b.Key) > 0 && b.Value.Type() != INVALID
}
