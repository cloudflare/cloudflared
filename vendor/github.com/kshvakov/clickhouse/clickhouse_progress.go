package clickhouse

type progress struct {
	rows      uint64
	bytes     uint64
	totalRows uint64
}

func (ch *clickhouse) progress() (*progress, error) {
	var (
		p   progress
		err error
	)
	if p.rows, err = ch.decoder.Uvarint(); err != nil {
		return nil, err
	}
	if p.bytes, err = ch.decoder.Uvarint(); err != nil {
		return nil, err
	}

	if p.totalRows, err = ch.decoder.Uvarint(); err != nil {
		return nil, err
	}

	return &p, nil
}
