Skittles
========

Miminal package for terminal colors/ANSI escape code.

![alt tag](https://raw.githubusercontent.com/acmacalister/skittles/master/pictures/terminal-colors.png)

## Install

`go get github.com/acmacalister/skittles`

`import "github.com/acmacalister/skittles"`

## Example

```go
package main

import (
  "fmt"
  "github.com/acmacalister/skittles"
)

func main() {
  fmt.Println(skittles.Red("Red's my favorite color"))
}
```

## Supported Platforms

Only tested on OS X terminal app, but I would expect it to work with any unix based terminal.

## Docs

* [GoDoc](http://godoc.org/github.com/acmacalister/skittles)

## Help

* [Github](https://github.com/acmacalister)
* [Twitter](http://twitter.com/acmacalister)
