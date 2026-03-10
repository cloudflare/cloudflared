// Copied from https://github.com/open-telemetry/opentelemetry-go/blob/cc43e01c27892252aac9a8f20da28cdde957a289/attribute/rawhelpers.go
// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package attribute

import (
	"math"
)

func boolToRaw(b bool) uint64 { // b is not a control flag.
	if b {
		return 1
	}
	return 0
}

func rawToBool(r uint64) bool {
	return r != 0
}

func int64ToRaw(i int64) uint64 {
	// Assumes original was a valid int64 (overflow not checked).
	return uint64(i) // nolint: gosec
}

func rawToInt64(r uint64) int64 {
	// Assumes original was a valid int64 (overflow not checked).
	return int64(r) // nolint: gosec
}

func float64ToRaw(f float64) uint64 {
	return math.Float64bits(f)
}

func rawToFloat64(r uint64) float64 {
	return math.Float64frombits(r)
}
