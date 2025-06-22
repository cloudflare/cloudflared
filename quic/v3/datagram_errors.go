package v3

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidDatagramType                 error = errors.New("invalid datagram type expected")
	ErrDatagramHeaderTooSmall              error = fmt.Errorf("datagram should have at least %d byte", datagramTypeLen)
	ErrDatagramPayloadTooLarge             error = errors.New("payload length is too large to be bundled in datagram")
	ErrDatagramPayloadHeaderTooSmall       error = errors.New("payload length is too small to fit the datagram header")
	ErrDatagramPayloadInvalidSize          error = errors.New("datagram provided is an invalid size")
	ErrDatagramResponseMsgInvalidSize      error = errors.New("datagram response message is an invalid size")
	ErrDatagramResponseInvalidSize         error = errors.New("datagram response is an invalid size")
	ErrDatagramResponseMsgTooLargeMaximum  error = fmt.Errorf("datagram response error message length exceeds the length of the datagram maximum: %d", maxResponseErrorMessageLen)
	ErrDatagramResponseMsgTooLargeDatagram error = fmt.Errorf("datagram response error message length exceeds the length of the provided datagram")
	ErrDatagramICMPPayloadTooLarge         error = fmt.Errorf("datagram icmp payload exceeds %d bytes", maxICMPPayloadLen)
	ErrDatagramICMPPayloadMissing          error = errors.New("datagram icmp payload is missing")
)

func wrapMarshalErr(err error) error {
	return fmt.Errorf("datagram marshal error: %w", err)
}

func wrapUnmarshalErr(err error) error {
	return fmt.Errorf("datagram unmarshal error: %w", err)
}
