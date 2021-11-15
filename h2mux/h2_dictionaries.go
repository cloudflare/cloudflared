package h2mux

import (
	"bytes"
	"io"
	"strings"
	"sync"

	"golang.org/x/net/http2"
)

/* This is an implementation of https://github.com/vkrasnov/h2-compression-dictionaries
but modified for tunnels in a few key ways:
Since tunnels is a server-to-server service, some aspects of the spec would cause
unnecessary head-of-line blocking on the CPU and on the network, hence this implementation
allows for parallel compression on the "client", and buffering on the "server" to solve
this problem. */

// Assign temporary values
const SettingCompression http2.SettingID = 0xff20

const (
	FrameSetCompressionContext http2.FrameType = 0xf0
	FrameUseDictionary         http2.FrameType = 0xf1
	FrameSetDictionary         http2.FrameType = 0xf2
)

const (
	FlagSetDictionaryAppend http2.Flags = 0x1
	FlagSetDictionaryOffset http2.Flags = 0x2
)

const compressionVersion = uint8(1)
const compressionFormat = uint8(2)

type CompressionSetting uint

const (
	CompressionNone CompressionSetting = iota
	CompressionLow
	CompressionMedium
	CompressionMax
)

type CompressionPreset struct {
	nDicts, dictSize, quality uint8
}

type compressor interface {
	Write([]byte) (int, error)
	Flush() error
	SetDictionary([]byte)
	Close() error
}

type decompressor interface {
	Read([]byte) (int, error)
	SetDictionary([]byte)
	Close() error
}

var compressionPresets = map[CompressionSetting]CompressionPreset{
	CompressionNone:   {0, 0, 0},
	CompressionLow:    {32, 17, 5},
	CompressionMedium: {64, 18, 6},
	CompressionMax:    {255, 19, 9},
}

func compressionSettingVal(version, fmt, sz, nd uint8) uint32 {
	// Currently the compression settings are include:
	// * version: only 1 is supported
	// * fmt: only 2 for brotli is supported
	// * sz: log2 of the maximal allowed dictionary size
	// * nd: max allowed number of dictionaries
	return uint32(version)<<24 + uint32(fmt)<<16 + uint32(sz)<<8 + uint32(nd)
}

func parseCompressionSettingVal(setting uint32) (version, fmt, sz, nd uint8) {
	version = uint8(setting >> 24)
	fmt = uint8(setting >> 16)
	sz = uint8(setting >> 8)
	nd = uint8(setting)
	return
}

func (c CompressionSetting) toH2Setting() uint32 {
	p, ok := compressionPresets[c]
	if !ok {
		return 0
	}
	return compressionSettingVal(compressionVersion, compressionFormat, p.dictSize, p.nDicts)
}

func (c CompressionSetting) getPreset() CompressionPreset {
	return compressionPresets[c]
}

type dictUpdate struct {
	reader     *h2DictionaryReader
	dictionary *h2ReadDictionary
	buff       []byte
	isReady    bool
	isUse      bool
	s          setDictRequest
}

type h2ReadDictionary struct {
	dictionary []byte
	queue      []*dictUpdate
	maxSize    int
}

type h2ReadDictionaries struct {
	d       []h2ReadDictionary
	maxSize int
}

type h2DictionaryReader struct {
	*SharedBuffer                // Propagate the decompressed output into the original buffer
	decompBuffer   *bytes.Buffer // Intermediate buffer for the brotli compressor
	dictionary     []byte        // The content of the dictionary being used by this reader
	internalBuffer []byte
	s, e           int           // Start and end of the buffer
	decomp         decompressor  // The brotli compressor
	isClosed       bool          // Indicates that Close was called for this reader
	queue          []*dictUpdate // List of dictionaries to update, when the data is available
}

type h2WriteDictionary []byte

type setDictRequest struct {
	streamID         uint32
	dictID           uint8
	dictSZ           uint64
	truncate, offset uint64
	P, E, D          bool
}

type useDictRequest struct {
	dictID   uint8
	streamID uint32
	setDict  []setDictRequest
}

type h2WriteDictionaries struct {
	dictLock        sync.Mutex
	dictChan        chan useDictRequest
	dictionaries    []h2WriteDictionary
	nextAvail       int              // next unused dictionary slot
	maxAvail        int              // max ID, defined by SETTINGS
	maxSize         int              // max size, defined by SETTINGS
	typeToDict      map[string]uint8 // map from content type to dictionary that encodes it
	pathToDict      map[string]uint8 // map from path to dictionary that encodes it
	quality         int
	window          int
	compIn, compOut *AtomicCounter
}

type h2DictWriter struct {
	*bytes.Buffer
	comp       compressor
	dicts      *h2WriteDictionaries
	writerLock sync.Mutex

	streamID    uint32
	path        string
	contentType string
}

type h2Dictionaries struct {
	write *h2WriteDictionaries
	read  *h2ReadDictionaries
}

func (o *dictUpdate) update(buff []byte) {
	o.buff = make([]byte, len(buff))
	copy(o.buff, buff)
	o.isReady = true
}

func (d *h2ReadDictionary) update() {
	for len(d.queue) > 0 {
		o := d.queue[0]
		if !o.isReady {
			break
		}
		if o.isUse {
			reader := o.reader
			reader.dictionary = make([]byte, len(d.dictionary))
			copy(reader.dictionary, d.dictionary)
			reader.decomp = newDecompressor(reader.decompBuffer)
			if len(reader.dictionary) > 0 {
				reader.decomp.SetDictionary(reader.dictionary)
			}
			reader.Write([]byte{})
		} else {
			d.dictionary = adjustDictionary(d.dictionary, o.buff, o.s, d.maxSize)
		}
		d.queue = d.queue[1:]
	}
}

func newH2ReadDictionaries(nd, sz uint8) h2ReadDictionaries {
	d := make([]h2ReadDictionary, int(nd))
	for i := range d {
		d[i].maxSize = 1 << uint(sz)
	}
	return h2ReadDictionaries{d: d, maxSize: 1 << uint(sz)}
}

func (dicts *h2ReadDictionaries) getDictByID(dictID uint8) (*h2ReadDictionary, error) {
	if int(dictID) > len(dicts.d) {
		return nil, MuxerStreamError{"dictID too big", http2.ErrCodeProtocol}
	}

	return &dicts.d[dictID], nil
}

func (dicts *h2ReadDictionaries) newReader(b *SharedBuffer, dictID uint8) *h2DictionaryReader {
	if int(dictID) > len(dicts.d) {
		return nil
	}

	dictionary := &dicts.d[dictID]
	reader := &h2DictionaryReader{SharedBuffer: b, decompBuffer: &bytes.Buffer{}, internalBuffer: make([]byte, dicts.maxSize)}

	if len(dictionary.queue) == 0 {
		reader.dictionary = make([]byte, len(dictionary.dictionary))
		copy(reader.dictionary, dictionary.dictionary)
		reader.decomp = newDecompressor(reader.decompBuffer)
		if len(reader.dictionary) > 0 {
			reader.decomp.SetDictionary(reader.dictionary)
		}
	} else {
		dictionary.queue = append(dictionary.queue, &dictUpdate{isUse: true, isReady: true, reader: reader})
	}
	return reader
}

func (r *h2DictionaryReader) updateWaitingDictionaries() {
	// Update all the waiting dictionaries
	for _, o := range r.queue {
		if o.isReady {
			continue
		}
		if r.isClosed || uint64(r.e) >= o.s.dictSZ {
			o.update(r.internalBuffer[:r.e])
			if o == o.dictionary.queue[0] {
				defer o.dictionary.update()
			}
		}
	}
}

// Write actually happens when reading from network, this is therefore the stage where we decompress the buffer
func (r *h2DictionaryReader) Write(p []byte) (n int, err error) {
	// Every write goes into brotli buffer first
	n, err = r.decompBuffer.Write(p)
	if err != nil {
		return
	}

	if r.decomp == nil {
		return
	}

	for {
		m, err := r.decomp.Read(r.internalBuffer[r.e:])
		if err != nil && err != io.EOF {
			r.SharedBuffer.Close()
			r.decomp.Close()
			return n, err
		}

		r.SharedBuffer.Write(r.internalBuffer[r.e : r.e+m])
		r.e += m

		if m == 0 {
			break
		}

		if r.e == len(r.internalBuffer) {
			r.updateWaitingDictionaries()
			r.e = 0
		}
	}

	r.updateWaitingDictionaries()

	if r.isClosed {
		r.SharedBuffer.Close()
		r.decomp.Close()
	}

	return
}

func (r *h2DictionaryReader) Close() error {
	if r.isClosed {
		return nil
	}
	r.isClosed = true
	r.Write([]byte{})
	return nil
}

var compressibleTypes = map[string]bool{
	"application/atom+xml":                true,
	"application/javascript":              true,
	"application/json":                    true,
	"application/ld+json":                 true,
	"application/manifest+json":           true,
	"application/rss+xml":                 true,
	"application/vnd.geo+json":            true,
	"application/vnd.ms-fontobject":       true,
	"application/x-font-ttf":              true,
	"application/x-yaml":                  true,
	"application/x-web-app-manifest+json": true,
	"application/xhtml+xml":               true,
	"application/xml":                     true,
	"font/opentype":                       true,
	"image/bmp":                           true,
	"image/svg+xml":                       true,
	"image/x-icon":                        true,
	"text/cache-manifest":                 true,
	"text/css":                            true,
	"text/html":                           true,
	"text/plain":                          true,
	"text/vcard":                          true,
	"text/vnd.rim.location.xloc":          true,
	"text/vtt":                            true,
	"text/x-component":                    true,
	"text/x-cross-domain-policy":          true,
	"text/x-yaml":                         true,
}

func getContentType(headers []Header) string {
	for _, h := range headers {
		if strings.ToLower(h.Name) == "content-type" {
			val := strings.ToLower(h.Value)
			sep := strings.IndexRune(val, ';')
			if sep != -1 {
				return val[:sep]
			}
			return val
		}
	}

	return ""
}

func newH2WriteDictionaries(nd, sz, quality uint8, compIn, compOut *AtomicCounter) (*h2WriteDictionaries, chan useDictRequest) {
	useDictChan := make(chan useDictRequest)
	return &h2WriteDictionaries{
		dictionaries: make([]h2WriteDictionary, nd),
		nextAvail:    0,
		maxAvail:     int(nd),
		maxSize:      1 << uint(sz),
		dictChan:     useDictChan,
		typeToDict:   make(map[string]uint8),
		pathToDict:   make(map[string]uint8),
		quality:      int(quality),
		window:       1 << uint(sz+1),
		compIn:       compIn,
		compOut:      compOut,
	}, useDictChan
}

func adjustDictionary(currentDictionary, newData []byte, set setDictRequest, maxSize int) []byte {
	currentDictionary = append(currentDictionary, newData[:set.dictSZ]...)

	if len(currentDictionary) > maxSize {
		currentDictionary = currentDictionary[len(currentDictionary)-maxSize:]
	}

	return currentDictionary
}

func (h2d *h2WriteDictionaries) getNextDictID() (dictID uint8, ok bool) {
	if h2d.nextAvail < h2d.maxAvail {
		dictID, ok = uint8(h2d.nextAvail), true
		h2d.nextAvail++
		return
	}

	return 0, false
}

func (h2d *h2WriteDictionaries) getGenericDictID() (dictID uint8, ok bool) {
	if h2d.maxAvail == 0 {
		return 0, false
	}
	return uint8(h2d.maxAvail - 1), true
}

func (h2d *h2WriteDictionaries) getDictWriter(s *MuxedStream, headers []Header) *h2DictWriter {
	w := s.writeBuffer

	if w == nil {
		return nil
	}

	if s.method != "GET" && s.method != "POST" {
		return nil
	}

	s.contentType = getContentType(headers)
	if _, ok := compressibleTypes[s.contentType]; !ok && !strings.HasPrefix(s.contentType, "text") {
		return nil
	}

	return &h2DictWriter{
		Buffer:      w.(*bytes.Buffer),
		path:        s.path,
		contentType: s.contentType,
		streamID:    s.streamID,
		dicts:       h2d,
	}
}

func assignDictToStream(s *MuxedStream, p []byte) bool {

	// On first write to stream:
	// * assign the right dictionary
	// * update relevant dictionaries
	// * send the required USE_DICT and SET_DICT frames

	h2d := s.dictionaries.write
	if h2d == nil {
		return false
	}

	w, ok := s.writeBuffer.(*h2DictWriter)
	if !ok || w.comp != nil {
		return false
	}

	h2d.dictLock.Lock()

	if w.comp != nil {
		// Check again with lock, in therory the interface allows for unordered writes
		h2d.dictLock.Unlock()
		return false
	}

	// The logic of dictionary generation is below

	// Is there a dictionary for the exact path or content-type?
	var useID uint8
	pathID, pathFound := h2d.pathToDict[w.path]
	typeID, typeFound := h2d.typeToDict[w.contentType]

	if pathFound {
		// Use dictionary for path as top priority
		useID = pathID
		if !typeFound { // Shouldn't really happen, unless type changes between requests
			typeID, typeFound = h2d.getNextDictID()
			if typeFound {
				h2d.typeToDict[w.contentType] = typeID
			}
		}
	} else if typeFound {
		// Use dictionary for same content type as second priority
		useID = typeID
		pathID, pathFound = h2d.getNextDictID()
		if pathFound { // If a slot is available, generate new dictionary for path
			h2d.pathToDict[w.path] = pathID
		}
	} else {
		// Use the overflow dictionary as last resort
		// If slots are available generate new dictionaries for path and content-type
		useID, _ = h2d.getGenericDictID()
		pathID, pathFound = h2d.getNextDictID()
		if pathFound {
			h2d.pathToDict[w.path] = pathID
		}
		typeID, typeFound = h2d.getNextDictID()
		if typeFound {
			h2d.typeToDict[w.contentType] = typeID
		}
	}

	useLen := h2d.maxSize
	if len(p) < useLen {
		useLen = len(p)
	}

	// Update all the dictionaries using the new data
	setDicts := make([]setDictRequest, 0, 3)
	setDict := setDictRequest{
		streamID: w.streamID,
		dictID:   useID,
		dictSZ:   uint64(useLen),
	}
	setDicts = append(setDicts, setDict)
	if pathID != useID {
		setDict.dictID = pathID
		setDicts = append(setDicts, setDict)
	}
	if typeID != useID {
		setDict.dictID = typeID
		setDicts = append(setDicts, setDict)
	}

	h2d.dictChan <- useDictRequest{streamID: w.streamID, dictID: uint8(useID), setDict: setDicts}

	dict := h2d.dictionaries[useID]

	// Brolti requires the dictionary to be immutable
	copyDict := make([]byte, len(dict))
	copy(copyDict, dict)

	for _, set := range setDicts {
		h2d.dictionaries[set.dictID] = adjustDictionary(h2d.dictionaries[set.dictID], p, set, h2d.maxSize)
	}

	w.comp = newCompressor(w.Buffer, h2d.quality, h2d.window)

	s.writeLock.Lock()
	h2d.dictLock.Unlock()

	if len(copyDict) > 0 {
		w.comp.SetDictionary(copyDict)
	}

	return true
}

func (w *h2DictWriter) Write(p []byte) (n int, err error) {
	bufLen := w.Buffer.Len()
	if w.comp != nil {
		n, err = w.comp.Write(p)
		if err != nil {
			return
		}
		err = w.comp.Flush()
		w.dicts.compIn.IncrementBy(uint64(n))
		w.dicts.compOut.IncrementBy(uint64(w.Buffer.Len() - bufLen))
		return
	}
	return w.Buffer.Write(p)
}

func (w *h2DictWriter) Close() error {
	if w.comp != nil {
		return w.comp.Close()
	}
	return nil
}

// From http2/hpack
func http2ReadVarInt(n byte, p []byte) (remain []byte, v uint64, err error) {
	if n < 1 || n > 8 {
		panic("bad n")
	}
	if len(p) == 0 {
		return nil, 0, MuxerStreamError{"unexpected EOF", http2.ErrCodeProtocol}
	}
	v = uint64(p[0])
	if n < 8 {
		v &= (1 << uint64(n)) - 1
	}
	if v < (1<<uint64(n))-1 {
		return p[1:], v, nil
	}

	origP := p
	p = p[1:]
	var m uint64
	for len(p) > 0 {
		b := p[0]
		p = p[1:]
		v += uint64(b&127) << m
		if b&128 == 0 {
			return p, v, nil
		}
		m += 7
		if m >= 63 {
			return origP, 0, MuxerStreamError{"invalid integer", http2.ErrCodeProtocol}
		}
	}
	return nil, 0, MuxerStreamError{"unexpected EOF", http2.ErrCodeProtocol}
}

func appendVarInt(dst []byte, n byte, i uint64) []byte {
	k := uint64((1 << n) - 1)
	if i < k {
		return append(dst, byte(i))
	}
	dst = append(dst, byte(k))
	i -= k
	for ; i >= 128; i >>= 7 {
		dst = append(dst, byte(0x80|(i&0x7f)))
	}
	return append(dst, byte(i))
}
