package data

import (
	"fmt"

	"github.com/kshvakov/clickhouse/lib/binary"
)

const ClientName = "Golang SQLDriver"

const (
	ClickHouseRevision         = 54213
	ClickHouseDBMSVersionMajor = 1
	ClickHouseDBMSVersionMinor = 1
)

type ClientInfo struct{}

func (ClientInfo) Write(encoder *binary.Encoder) error {
	encoder.String(ClientName)
	encoder.Uvarint(ClickHouseDBMSVersionMajor)
	encoder.Uvarint(ClickHouseDBMSVersionMinor)
	encoder.Uvarint(ClickHouseRevision)
	return nil
}

func (ClientInfo) String() string {
	return fmt.Sprintf("%s %d.%d.%d", ClientName, ClickHouseDBMSVersionMajor, ClickHouseDBMSVersionMinor, ClickHouseRevision)
}
