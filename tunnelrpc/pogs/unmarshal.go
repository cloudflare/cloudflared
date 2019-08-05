package pogs

import (
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"
)

// ScopeUnmarshaler can marshal a Scope pog from JSON.
type ScopeUnmarshaler struct {
	Scope Scope
}

// UnmarshalJSON takes in a JSON string, and attempts to marshal it into a Scope.
// If successful, the Scope member of this ScopeUnmarshaler is set and nil is returned.
// If unsuccessful, returns an error.
func (su *ScopeUnmarshaler) UnmarshalJSON(b []byte) error {
	var scopeJSON map[string]interface{}
	if err := json.Unmarshal(b, &scopeJSON); err != nil {
		return errors.Wrapf(err, "cannot unmarshal %s into scopeJSON", string(b))
	}

	if group, ok := scopeJSON["group"]; ok {
		if val, ok := group.(string); ok {
			su.Scope = NewGroup(val)
			return nil
		}
		return fmt.Errorf("JSON should have been a Scope, but the 'group' key contained %v", group)
	}

	if systemName, ok := scopeJSON["system_name"]; ok {
		if val, ok := systemName.(string); ok {
			su.Scope = NewSystemName(val)
			return nil
		}
		return fmt.Errorf("JSON should have been a Scope, but the 'system_name' key contained %v", systemName)
	}

	return fmt.Errorf("JSON should have been an object with one root key, either 'system_name' or 'group'")
}
