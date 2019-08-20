package leakypool

var pool chan []byte

func InitBytePool(size int) {
	pool = make(chan []byte, size)
}

func GetBytes(size, capacity int) (b []byte) {
	select {
	case b = <-pool:
	default:
		b = make([]byte, size, capacity)
	}
	return
}

func PutBytes(b []byte) {
	select {
	case pool <- b:
	default:
	}
}
