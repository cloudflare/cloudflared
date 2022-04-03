package qrterminal

import (
	"io"
	"strings"

	"rsc.io/qr"
)

const WHITE = "\033[47m  \033[0m"
const BLACK = "\033[40m  \033[0m"

// Use ascii blocks to form the QR Code
const BLACK_WHITE = "▄"
const BLACK_BLACK = " "
const WHITE_BLACK = "▀"
const WHITE_WHITE = "█"

// Level - the QR Code's redundancy level
const H = qr.H
const M = qr.M
const L = qr.L

// default is 4-pixel-wide white quiet zone
const QUIET_ZONE = 4

//Config for generating a barcode
type Config struct {
	Level          qr.Level
	Writer         io.Writer
	HalfBlocks     bool
	BlackChar      string
	BlackWhiteChar string
	WhiteChar      string
	WhiteBlackChar string
	QuietZone      int
}

func (c *Config) writeFullBlocks(w io.Writer, code *qr.Code) {
	white := c.WhiteChar
	black := c.BlackChar

	// Frame the barcode in a 1 pixel border
	w.Write([]byte(stringRepeat(stringRepeat(white,
		code.Size+c.QuietZone*2)+"\n", c.QuietZone))) // top border
	for i := 0; i <= code.Size; i++ {
		w.Write([]byte(stringRepeat(white, c.QuietZone))) // left border
		for j := 0; j <= code.Size; j++ {
			if code.Black(j, i) {
				w.Write([]byte(black))
			} else {
				w.Write([]byte(white))
			}
		}
		w.Write([]byte(stringRepeat(white, c.QuietZone-1) + "\n")) // right border
	}
	w.Write([]byte(stringRepeat(stringRepeat(white,
		code.Size+c.QuietZone*2)+"\n", c.QuietZone-1))) // bottom border
}

func (c *Config) writeHalfBlocks(w io.Writer, code *qr.Code) {
	ww := c.WhiteChar
	bb := c.BlackChar
	wb := c.WhiteBlackChar
	bw := c.BlackWhiteChar
	// Frame the barcode in a 4 pixel border
	// top border
	if c.QuietZone%2 != 0 {
		w.Write([]byte(stringRepeat(bw, code.Size+c.QuietZone*2) + "\n"))
		w.Write([]byte(stringRepeat(stringRepeat(ww,
			code.Size+c.QuietZone*2)+"\n", c.QuietZone/2)))
	} else {
		w.Write([]byte(stringRepeat(stringRepeat(ww,
			code.Size+c.QuietZone*2)+"\n", c.QuietZone/2)))
	}
	for i := 0; i <= code.Size; i += 2 {
		w.Write([]byte(stringRepeat(ww, c.QuietZone))) // left border
		for j := 0; j <= code.Size; j++ {
			next_black := false
			if i+1 < code.Size {
				next_black = code.Black(j, i+1)
			}
			curr_black := code.Black(j, i)
			if curr_black && next_black {
				w.Write([]byte(bb))
			} else if curr_black && !next_black {
				w.Write([]byte(bw))
			} else if !curr_black && !next_black {
				w.Write([]byte(ww))
			} else {
				w.Write([]byte(wb))
			}
		}
		w.Write([]byte(stringRepeat(ww, c.QuietZone-1) + "\n")) // right border
	}
	// bottom border
	if c.QuietZone%2 == 0 {
		w.Write([]byte(stringRepeat(stringRepeat(ww,
			code.Size+c.QuietZone*2)+"\n", c.QuietZone/2-1)))
		w.Write([]byte(stringRepeat(wb, code.Size+c.QuietZone*2) + "\n"))
	} else {
		w.Write([]byte(stringRepeat(stringRepeat(ww,
			code.Size+c.QuietZone*2)+"\n", c.QuietZone/2)))
	}
}

func stringRepeat(s string, count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(s, count)
}

// GenerateWithConfig expects a string to encode and a config
func GenerateWithConfig(text string, config Config) {
	if config.QuietZone < 1 {
		config.QuietZone = 1 // at least 1-pixel-wide white quiet zone
	}
	w := config.Writer
	code, _ := qr.Encode(text, config.Level)
	if config.HalfBlocks {
		config.writeHalfBlocks(w, code)
	} else {
		config.writeFullBlocks(w, code)
	}
}

// Generate a QR Code and write it out to io.Writer
func Generate(text string, l qr.Level, w io.Writer) {
	config := Config{
		Level:     l,
		Writer:    w,
		BlackChar: BLACK,
		WhiteChar: WHITE,
		QuietZone: QUIET_ZONE,
	}
	GenerateWithConfig(text, config)
}

// Generate a QR Code with half blocks and write it out to io.Writer
func GenerateHalfBlock(text string, l qr.Level, w io.Writer) {
	config := Config{
		Level:          l,
		Writer:         w,
		HalfBlocks:     true,
		BlackChar:      BLACK_BLACK,
		WhiteBlackChar: WHITE_BLACK,
		WhiteChar:      WHITE_WHITE,
		BlackWhiteChar: BLACK_WHITE,
		QuietZone:      QUIET_ZONE,
	}
	GenerateWithConfig(text, config)
}
