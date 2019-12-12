package clickhouse

import "errors"

type result struct{}

func (*result) LastInsertId() (int64, error) { return 0, errors.New("LastInsertId is not supported") }
func (*result) RowsAffected() (int64, error) { return 0, errors.New("RowsAffected is not supported") }
