// The MIT License
//
// Copyright (c) 2019-2020, Cloudflare, Inc. and Apple, Inc. All rights reserved.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package odoh

import (
	"encoding/binary"
	"fmt"
)

func encodeLengthPrefixedSlice(slice []byte) []byte {
	result := make([]byte, 2)
	binary.BigEndian.PutUint16(result, uint16(len(slice)))
	return append(result, slice...)
}

func decodeLengthPrefixedSlice(slice []byte) ([]byte, int, error) {
	if len(slice) < 2 {
		return nil, 0, fmt.Errorf("Expected at least 2 bytes of length encoded prefix")
	}

	length := binary.BigEndian.Uint16(slice)
	if int(2+length) > len(slice) {
		return nil, 0, fmt.Errorf("Insufficient data. Expected %d, got %d", 2+length, len(slice))
	}

	return slice[2 : 2+length], int(2 + length), nil
}
