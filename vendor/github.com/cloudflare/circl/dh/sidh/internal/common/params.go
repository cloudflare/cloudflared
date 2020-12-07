package common

import "fmt"

// Keeps mapping: SIDH prime field ID to domain parameters
var sidhParams = make(map[uint8]SidhParams)

// Params returns domain parameters corresponding to finite field and identified by
// `id` provieded by the caller. Function panics in case `id` wasn't registered earlier.
func Params(id uint8) *SidhParams {
	if val, ok := sidhParams[id]; ok {
		return &val
	}
	panic("sidh: SIDH Params ID unregistered")
}

// Registers SIDH parameters for particular field.
func Register(id uint8, p *SidhParams) {
	if _, ok := sidhParams[id]; ok {
		msg := fmt.Sprintf("sidh: Field with id %d already registered", id)
		panic(msg)
	}
	sidhParams[id] = *p
}
