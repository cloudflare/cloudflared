package h2mux

import (
	"bytes"
	"io"
	"sync"
)

type SharedBuffer struct {
	cond   *sync.Cond
	buffer bytes.Buffer
	eof    bool
}

func NewSharedBuffer() *SharedBuffer {
	return &SharedBuffer{
		cond: sync.NewCond(&sync.Mutex{}),
	}
}

func (s *SharedBuffer) Read(p []byte) (n int, err error) {
	totalRead := 0
	s.cond.L.Lock()
	for totalRead == 0 {
		n, err = s.buffer.Read(p[totalRead:])
		totalRead += n
		if err == io.EOF {
			if s.eof {
				break
			}
			err = nil
			if n > 0 {
				break
			}
			s.cond.Wait()
		}
	}
	s.cond.L.Unlock()
	return totalRead, err
}

func (s *SharedBuffer) Write(p []byte) (n int, err error) {
	s.cond.L.Lock()
	defer s.cond.L.Unlock()
	if s.eof {
		return 0, io.EOF
	}
	n, err = s.buffer.Write(p)
	s.cond.Signal()
	return
}

func (s *SharedBuffer) Close() error {
	s.cond.L.Lock()
	defer s.cond.L.Unlock()
	if !s.eof {
		s.eof = true
		s.cond.Signal()
	}
	return nil
}

func (s *SharedBuffer) Closed() bool {
	s.cond.L.Lock()
	defer s.cond.L.Unlock()
	return s.eof
}
