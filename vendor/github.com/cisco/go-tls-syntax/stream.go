package syntax

///
/// Write Stream
///

type WriteStream struct {
	buffer []byte
}

func NewWriteStream() *WriteStream {
	return &WriteStream{}
}

func (s *WriteStream) Data() []byte {
	return s.buffer
}

func (s *WriteStream) Write(val interface{}) error {
	enc, err := Marshal(val)
	if err != nil {
		return err
	}
	s.buffer = append(s.buffer, enc...)
	return nil
}

func (s *WriteStream) WriteAll(vals ...interface{}) error {
	for _, val := range vals {
		err := s.Write(val)
		if err != nil {
			return err
		}
	}
	return nil
}

///
/// ReadStream
///

type ReadStream struct {
	buffer []byte
	cursor int
}

func NewReadStream(data []byte) *ReadStream {
	return &ReadStream{data, 0}
}

func (s *ReadStream) Read(val interface{}) (int, error) {
	read, err := Unmarshal(s.buffer[s.cursor:], val)
	if err != nil {
		return 0, err
	}

	s.cursor += read
	return read, nil
}

func (s *ReadStream) ReadAll(vals ...interface{}) (int, error) {
	read := 0
	for _, val := range vals {
		readHere, err := s.Read(val)
		if err != nil {
			return 0, err
		}

		read += readHere
	}
	return read, nil
}

func (s *ReadStream) Position() int {
	return s.cursor
}
