// Package uniq keeps track of "thing" that are either "todo" or "done". Multiple
// identical events will only be processed once.
package uniq

// U keeps track of item to be done.
type U struct {
	u map[string]item
}

type item struct {
	state int          // either todo or done
	f     func() error // function to be executed.
	obj   interface{}  // any object to return when needed
}

// New returns a new initialized U.
func New() U { return U{u: make(map[string]item)} }

// Set sets function f in U under key. If the key already exists
// it is not overwritten.
func (u U) Set(key string, f func() error, o interface{}) interface{} {
	if item, ok := u.u[key]; ok {
		return item.obj
	}
	u.u[key] = item{todo, f, o}
	return o
}

// Unset removes the key.
func (u U) Unset(key string) {
	if _, ok := u.u[key]; ok {
		delete(u.u, key)
	}
}

// ForEach iterates for u executes f for each element that is 'todo' and sets it to 'done'.
func (u U) ForEach() error {
	for k, v := range u.u {
		if v.state == todo {
			v.f()
		}
		v.state = done
		u.u[k] = v
	}
	return nil
}

const (
	todo = 1
	done = 2
)
