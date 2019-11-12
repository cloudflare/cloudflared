package dbconnect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Client is an interface to talk to any database.
//
// Currently, the only implementation is SQLClient, but its structure
// should be designed to handle a MongoClient or RedisClient in the future.
type Client interface {
	Ping(context.Context) error
	Submit(context.Context, *Command) (interface{}, error)
}

// NewClient creates a database client based on its URL scheme.
func NewClient(ctx context.Context, originURL *url.URL) (Client, error) {
	return NewSQLClient(ctx, originURL)
}

// Command is a standard, non-vendor format for submitting database commands.
//
// When determining the scope of this struct, refer to the following litmus test:
// Could this (roughly) conform to SQL, Document-based, and Key-value command formats?
type Command struct {
	Statement string        `json:"statement"`
	Arguments Arguments     `json:"arguments,omitempty"`
	Mode      string        `json:"mode,omitempty"`
	Isolation string        `json:"isolation,omitempty"`
	Timeout   time.Duration `json:"timeout,omitempty"`
}

// Validate enforces the contract of Command: non empty statement (both in length and logic),
// lowercase mode and isolation, non-zero timeout, and valid Arguments.
func (cmd *Command) Validate() error {
	if cmd.Statement == "" {
		return fmt.Errorf("cannot provide an empty statement")
	}

	if strings.Map(func(char rune) rune {
		if char == ';' || unicode.IsSpace(char) {
			return -1
		}
		return char
	}, cmd.Statement) == "" {
		return fmt.Errorf("cannot provide a statement with no logic: '%s'", cmd.Statement)
	}

	cmd.Mode = strings.ToLower(cmd.Mode)
	cmd.Isolation = strings.ToLower(cmd.Isolation)

	if cmd.Timeout.Nanoseconds() <= 0 {
		cmd.Timeout = 24 * time.Hour
	}

	return cmd.Arguments.Validate()
}

// UnmarshalJSON converts a byte representation of JSON into a Command, which is also validated.
func (cmd *Command) UnmarshalJSON(data []byte) error {
	// Alias is required to avoid infinite recursion from the default UnmarshalJSON.
	type Alias Command
	alias := &struct {
		*Alias
	}{
		Alias: (*Alias)(cmd),
	}

	err := json.Unmarshal(data, &alias)
	if err == nil {
		err = cmd.Validate()
	}

	return err
}

// Arguments is a wrapper for either map-based or array-based Command arguments.
//
// Each field is mutually-exclusive and some Client implementations may not
// support both fields (eg. MySQL does not accept named arguments).
type Arguments struct {
	Named      map[string]interface{}
	Positional []interface{}
}

// Validate enforces the contract of Arguments: non nil, mutually exclusive, and no empty or reserved keys.
func (args *Arguments) Validate() error {
	if args.Named == nil {
		args.Named = map[string]interface{}{}
	}
	if args.Positional == nil {
		args.Positional = []interface{}{}
	}

	if len(args.Named) > 0 && len(args.Positional) > 0 {
		return fmt.Errorf("both named and positional arguments cannot be specified: %+v and %+v", args.Named, args.Positional)
	}

	for key := range args.Named {
		if key == "" {
			return fmt.Errorf("named arguments cannot contain an empty key: %+v", args.Named)
		}
		if !utf8.ValidString(key) {
			return fmt.Errorf("named argument does not conform to UTF-8 encoding: %s", key)
		}
		if strings.HasPrefix(key, "_") {
			return fmt.Errorf("named argument cannot start with a reserved keyword '_': %s", key)
		}
		if unicode.IsNumber([]rune(key)[0]) {
			return fmt.Errorf("named argument cannot start with a number: %s", key)
		}
	}

	return nil
}

// UnmarshalJSON converts a byte representation of JSON into Arguments, which is also validated.
func (args *Arguments) UnmarshalJSON(data []byte) error {
	var obj interface{}
	err := json.Unmarshal(data, &obj)
	if err != nil {
		return err
	}

	named, ok := obj.(map[string]interface{})
	if ok {
		args.Named = named
	} else {
		positional, ok := obj.([]interface{})
		if ok {
			args.Positional = positional
		} else {
			return fmt.Errorf("arguments must either be an object {\"0\":\"val\"} or an array [\"val\"]: %s", string(data))
		}
	}

	return args.Validate()
}
