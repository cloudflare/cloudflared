package clickhouse

import (
	"fmt"
	"strings"
)

type Exception struct {
	Code       int32
	Name       string
	Message    string
	StackTrace string
	nested     error
}

func (e *Exception) Error() string {
	return fmt.Sprintf("code: %d, message: %s", e.Code, e.Message)
}

func (ch *clickhouse) exception() error {
	defer ch.conn.Close()
	var (
		e         Exception
		err       error
		hasNested bool
	)
	if e.Code, err = ch.decoder.Int32(); err != nil {
		return err
	}
	if e.Name, err = ch.decoder.String(); err != nil {
		return err
	}
	if e.Message, err = ch.decoder.String(); err != nil {
		return err
	}
	e.Message = strings.TrimSpace(strings.TrimPrefix(e.Message, e.Name+":"))
	if e.StackTrace, err = ch.decoder.String(); err != nil {
		return err
	}
	if hasNested, err = ch.decoder.Bool(); err != nil {
		return err
	}
	if hasNested {
		e.nested = ch.exception()
	}
	return &e
}
