// +build go1.8

package sqlmock

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
)

// WillReturnRows specifies the set of resulting rows that will be returned
// by the triggered query
func (e *ExpectedQuery) WillReturnRows(rows ...*Rows) *ExpectedQuery {
	sets := make([]*Rows, len(rows))
	for i, r := range rows {
		sets[i] = r
	}
	e.rows = &rowSets{sets: sets, ex: e}
	return e
}

func (e *queryBasedExpectation) argsMatches(args []namedValue) error {
	if nil == e.args {
		return nil
	}
	if len(args) != len(e.args) {
		return fmt.Errorf("expected %d, but got %d arguments", len(e.args), len(args))
	}
	// @TODO should we assert either all args are named or ordinal?
	for k, v := range args {
		// custom argument matcher
		matcher, ok := e.args[k].(Argument)
		if ok {
			if !matcher.Match(v.Value) {
				return fmt.Errorf("matcher %T could not match %d argument %T - %+v", matcher, k, args[k], args[k])
			}
			continue
		}

		dval := e.args[k]
		if named, isNamed := dval.(sql.NamedArg); isNamed {
			dval = named.Value
			if v.Name != named.Name {
				return fmt.Errorf("named argument %d: name: \"%s\" does not match expected: \"%s\"", k, v.Name, named.Name)
			}
		} else if k+1 != v.Ordinal {
			return fmt.Errorf("argument %d: ordinal position: %d does not match expected: %d", k, k+1, v.Ordinal)
		}

		// convert to driver converter
		darg, err := e.converter.ConvertValue(dval)
		if err != nil {
			return fmt.Errorf("could not convert %d argument %T - %+v to driver value: %s", k, e.args[k], e.args[k], err)
		}

		if !driver.IsValue(darg) {
			return fmt.Errorf("argument %d: non-subset type %T returned from Value", k, darg)
		}

		if !reflect.DeepEqual(darg, v.Value) {
			return fmt.Errorf("argument %d expected [%T - %+v] does not match actual [%T - %+v]", k, darg, darg, v.Value, v.Value)
		}
	}
	return nil
}
