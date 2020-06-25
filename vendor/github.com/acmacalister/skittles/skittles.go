// Miminal package for terminal colors/ANSI escape code.
// Check out the source here https://github.com/acmacalister/skittles.
// Also see the example directory for another example on how to use skittles.
//
//  package main
//
//  import (
//    "fmt"
//    "github.com/acmacalister/skittles"
//  )
//
//  func main() {
//    fmt.Println(skittles.Red("Red's my favorite color"))
//  }
package skittles

import (
	"fmt"
	"strings"
)

// source: http://www.termsys.demon.co.uk/vtansi.htm
// source: http://ascii-table.com/ansi-escape-sequences.php

const (
	nofmt             = "0"
	bold              = "1"
	underline         = "4"
	blink             = "5"
	inverse           = "7"  // attributes end at 7
	black             = "30" // colors start at 30
	red               = "31"
	green             = "32"
	yellow            = "33"
	blue              = "34"
	magenta           = "35"
	cyan              = "36"
	white             = "37" // colors end at 37
	blackBackground   = "40" // background colors start at 40
	redBackground     = "41"
	greenBackground   = "42"
	yellowBackground  = "43"
	blueBackground    = "44"
	magentaBackground = "45"
	cyanBackground    = "46"
	whiteBackground   = "47"
)

// makeFunction returns a function that formats some text with the provided list
// of ANSI escape codes.
func makeFunction(attributes []string) func(interface{}) string {
	return func(text interface{}) string {
		return fmt.Sprintf("\033[%sm%s\033[0m", strings.Join(attributes, ";"), text)
	}
}

var (
	// Reset resets all formatting.
	Reset = makeFunction([]string{nofmt})
	// Bold makes terminal text bold and doesn't add any color.
	Bold = makeFunction([]string{bold})
	// Underline makes terminal text underlined and doesn't add any color.
	Underline = makeFunction([]string{underline})
	// Blink makes terminal text blink and doesn't add any color.
	Blink = makeFunction([]string{blink})
	// Inverse inverts terminal text and doesn't add any color.
	Inverse = makeFunction([]string{inverse})

	// Black makes terminal text black.
	Black = makeFunction([]string{black})
	// Red makes terminal text red.
	Red = makeFunction([]string{red})
	// Green makes terminal text green.
	Green = makeFunction([]string{green})
	// Yellow makes terminal text yellow.
	Yellow = makeFunction([]string{yellow})
	// Blue makes terminal text blue.
	Blue = makeFunction([]string{blue})
	// Magenta makes terminal text magenta.
	Magenta = makeFunction([]string{magenta})
	// Cyan makes terminal text cyan.
	Cyan = makeFunction([]string{cyan})
	// White makes terminal text white.
	White = makeFunction([]string{white})

	// BoldBlack makes terminal text bold and black.
	BoldBlack = makeFunction([]string{black, bold})
	// BoldRed makes terminal text bold and red.
	BoldRed = makeFunction([]string{red, bold})
	// BoldGreen makes terminal text bold and green.
	BoldGreen = makeFunction([]string{green, bold})
	// BoldYellow makes terminal text bold and yellow.
	BoldYellow = makeFunction([]string{yellow, bold})
	// BoldBlue makes terminal text bold and blue.
	BoldBlue = makeFunction([]string{blue, bold})
	// BoldMagenta makes terminal text bold and magenta.
	BoldMagenta = makeFunction([]string{magenta, bold})
	// BoldCyan makes terminal text bold and cyan.
	BoldCyan = makeFunction([]string{cyan, bold})
	// BoldWhite makes terminal text bold and white.
	BoldWhite = makeFunction([]string{white, bold})

	// BlinkBlack makes terminal text blink and black.
	BlinkBlack = makeFunction([]string{black, blink})
	// BlinkRed makes terminal text blink and red.
	BlinkRed = makeFunction([]string{red, blink})
	// BlinkGreen makes terminal text blink and green.
	BlinkGreen = makeFunction([]string{green, blink})
	// BlinkYellow makes terminal text blink and yellow.
	BlinkYellow = makeFunction([]string{yellow, blink})
	// BlinkBlue makes terminal text blink and blue.
	BlinkBlue = makeFunction([]string{blue, blink})
	// BlinkMagenta makes terminal text blink and magenta.
	BlinkMagenta = makeFunction([]string{magenta, blink})
	// BlinkCyan makes terminal text blink and cyan.
	BlinkCyan = makeFunction([]string{cyan, blink})
	// BlinkWhite makes terminal text blink and white.
	BlinkWhite = makeFunction([]string{white, blink})

	// UnderlineBlack makes terminal text underlined and black.
	UnderlineBlack = makeFunction([]string{black, underline})
	// UnderlineRed makes terminal text underlined and red.
	UnderlineRed = makeFunction([]string{red, underline})
	// UnderlineGreen makes terminal text underlined and green.
	UnderlineGreen = makeFunction([]string{green, underline})
	// UnderlineYellow makes terminal text underlined and yellow.
	UnderlineYellow = makeFunction([]string{yellow, underline})
	// UnderlineBlue makes terminal text underlined and blue.
	UnderlineBlue = makeFunction([]string{blue, underline})
	// UnderlineMagenta makes terminal text underlined and magenta.
	UnderlineMagenta = makeFunction([]string{magenta, underline})
	// UnderlineCyan makes terminal text underlined and cyan.
	UnderlineCyan = makeFunction([]string{cyan, underline})
	// UnderlineWhite makes terminal text underlined and white.
	UnderlineWhite = makeFunction([]string{white, underline})

	// InverseBlack makes terminal text inverted and black.
	InverseBlack = makeFunction([]string{black, inverse})
	// InverseRed makes terminal text inverted and red.
	InverseRed = makeFunction([]string{red, inverse})
	// InverseGreen makes terminal text inverted and green.
	InverseGreen = makeFunction([]string{green, inverse})
	// InverseYellow makes terminal text inverted and yellow.
	InverseYellow = makeFunction([]string{yellow, inverse})
	// InverseBlue makes terminal text inverted and blue.
	InverseBlue = makeFunction([]string{blue, inverse})
	// InverseMagenta makes terminal text inverted and magenta.
	InverseMagenta = makeFunction([]string{magenta, inverse})
	// InverseCyan makes terminal text inverted and cyan.
	InverseCyan = makeFunction([]string{cyan, inverse})
	// InverseWhite makes terminal text inverted and white.
	InverseWhite = makeFunction([]string{white, inverse})
)
