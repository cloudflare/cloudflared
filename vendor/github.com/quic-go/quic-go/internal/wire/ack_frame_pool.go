package wire

import "sync"

var ackFramePool = sync.Pool{New: func() any {
	return &AckFrame{}
}}

func GetAckFrame() *AckFrame {
	f := ackFramePool.Get().(*AckFrame)
	f.AckRanges = f.AckRanges[:0]
	f.ECNCE = 0
	f.ECT0 = 0
	f.ECT1 = 0
	f.DelayTime = 0
	return f
}

func PutAckFrame(f *AckFrame) {
	if cap(f.AckRanges) > 4 {
		return
	}
	ackFramePool.Put(f)
}
