// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
// Some code from the runtime/debug package of the Go standard library.

package raven

import (
	"bytes"
	"go/build"
	"io/ioutil"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/pkg/errors"
)

// https://docs.getsentry.com/hosted/clientdev/interfaces/#failure-interfaces
type Stacktrace struct {
	// Required
	Frames []*StacktraceFrame `json:"frames"`
}

func (s *Stacktrace) Class() string { return "stacktrace" }

func (s *Stacktrace) Culprit() string {
	for i := len(s.Frames) - 1; i >= 0; i-- {
		frame := s.Frames[i]
		if frame.InApp == true && frame.Module != "" && frame.Function != "" {
			return frame.Module + "." + frame.Function
		}
	}
	return ""
}

type StacktraceFrame struct {
	// At least one required
	Filename string `json:"filename,omitempty"`
	Function string `json:"function,omitempty"`
	Module   string `json:"module,omitempty"`

	// Optional
	Lineno       int      `json:"lineno,omitempty"`
	Colno        int      `json:"colno,omitempty"`
	AbsolutePath string   `json:"abs_path,omitempty"`
	ContextLine  string   `json:"context_line,omitempty"`
	PreContext   []string `json:"pre_context,omitempty"`
	PostContext  []string `json:"post_context,omitempty"`
	InApp        bool     `json:"in_app"`
}

// Try to get stacktrace from err as an interface of github.com/pkg/errors, or else NewStacktrace()
func GetOrNewStacktrace(err error, skip int, context int, appPackagePrefixes []string) *Stacktrace {
	stacktracer, errHasStacktrace := err.(interface {
		StackTrace() errors.StackTrace
	})
	if errHasStacktrace {
		var frames []*StacktraceFrame
		for _, f := range stacktracer.StackTrace() {
			pc := uintptr(f) - 1
			fn := runtime.FuncForPC(pc)
			var fName string
			var file string
			var line int
			if fn != nil {
				file, line = fn.FileLine(pc)
				fName = fn.Name()
			} else {
				file = "unknown"
				fName = "unknown"
			}
			frame := NewStacktraceFrame(pc, fName, file, line, context, appPackagePrefixes)
			if frame != nil {
				frames = append([]*StacktraceFrame{frame}, frames...)
			}
		}
		return &Stacktrace{Frames: frames}
	} else {
		return NewStacktrace(skip+1, context, appPackagePrefixes)
	}
}

// Intialize and populate a new stacktrace, skipping skip frames.
//
// context is the number of surrounding lines that should be included for context.
// Setting context to 3 would try to get seven lines. Setting context to -1 returns
// one line with no surrounding context, and 0 returns no context.
//
// appPackagePrefixes is a list of prefixes used to check whether a package should
// be considered "in app".
func NewStacktrace(skip int, context int, appPackagePrefixes []string) *Stacktrace {
	var frames []*StacktraceFrame

	callerPcs := make([]uintptr, 100)
	numCallers := runtime.Callers(skip+2, callerPcs)

	// If there are no callers, the entire stacktrace is nil
	if numCallers == 0 {
		return nil
	}

	callersFrames := runtime.CallersFrames(callerPcs)

	for {
		fr, more := callersFrames.Next()
		if fr.Func != nil {
			frame := NewStacktraceFrame(fr.PC, fr.Function, fr.File, fr.Line, context, appPackagePrefixes)
			if frame != nil {
				frames = append(frames, frame)
			}
		}
		if !more {
			break
		}
	}
	// If there are no frames, the entire stacktrace is nil
	if len(frames) == 0 {
		return nil
	}
	// Optimize the path where there's only 1 frame
	if len(frames) == 1 {
		return &Stacktrace{frames}
	}
	// Sentry wants the frames with the oldest first, so reverse them
	for i, j := 0, len(frames)-1; i < j; i, j = i+1, j-1 {
		frames[i], frames[j] = frames[j], frames[i]
	}
	return &Stacktrace{frames}
}

// Build a single frame using data returned from runtime.Caller.
//
// context is the number of surrounding lines that should be included for context.
// Setting context to 3 would try to get seven lines. Setting context to -1 returns
// one line with no surrounding context, and 0 returns no context.
//
// appPackagePrefixes is a list of prefixes used to check whether a package should
// be considered "in app".
func NewStacktraceFrame(pc uintptr, fName, file string, line, context int, appPackagePrefixes []string) *StacktraceFrame {
	frame := &StacktraceFrame{AbsolutePath: file, Filename: trimPath(file), Lineno: line, InApp: false}
	frame.Module, frame.Function = functionName(fName)

	// `runtime.goexit` is effectively a placeholder that comes from
	// runtime/asm_amd64.s and is meaningless.
	if frame.Module == "runtime" && frame.Function == "goexit" {
		return nil
	}

	if frame.Module == "main" {
		frame.InApp = true
	} else {
		for _, prefix := range appPackagePrefixes {
			if strings.HasPrefix(frame.Module, prefix) && !strings.Contains(frame.Module, "vendor") && !strings.Contains(frame.Module, "third_party") {
				frame.InApp = true
			}
		}
	}

	if context > 0 {
		contextLines, lineIdx := sourceCodeLoader.Load(file, line, context)
		if len(contextLines) > 0 {
			for i, line := range contextLines {
				switch {
				case i < lineIdx:
					frame.PreContext = append(frame.PreContext, string(line))
				case i == lineIdx:
					frame.ContextLine = string(line)
				default:
					frame.PostContext = append(frame.PostContext, string(line))
				}
			}
		}
	} else if context == -1 {
		contextLine, _ := sourceCodeLoader.Load(file, line, 0)
		if len(contextLine) > 0 {
			frame.ContextLine = string(contextLine[0])
		}
	}
	return frame
}

// Retrieve the name of the package and function containing the PC.
func functionName(fName string) (pack string, name string) {
	name = fName
	// We get this:
	//	runtime/debug.*T·ptrmethod
	// and want this:
	//  pack = runtime/debug
	//	name = *T.ptrmethod
	if idx := strings.LastIndex(name, "."); idx != -1 {
		pack = name[:idx]
		name = name[idx+1:]
	}
	name = strings.Replace(name, "·", ".", -1)
	return
}

type SourceCodeLoader interface {
	Load(filename string, line, context int) ([][]byte, int)
}

var sourceCodeLoader SourceCodeLoader = &fsLoader{cache: make(map[string][][]byte)}

func SetSourceCodeLoader(loader SourceCodeLoader) {
	sourceCodeLoader = loader
}

type fsLoader struct {
	mu    sync.Mutex
	cache map[string][][]byte
}

func (fs *fsLoader) Load(filename string, line, context int) ([][]byte, int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	lines, ok := fs.cache[filename]
	if !ok {
		data, err := ioutil.ReadFile(filename)
		if err != nil {
			// cache errors as nil slice: code below handles it correctly
			// otherwise when missing the source or running as a different user, we try
			// reading the file on each error which is unnecessary
			fs.cache[filename] = nil
			return nil, 0
		}
		lines = bytes.Split(data, []byte{'\n'})
		fs.cache[filename] = lines
	}

	if lines == nil {
		// cached error from ReadFile: return no lines
		return nil, 0
	}

	line-- // stack trace lines are 1-indexed
	start := line - context
	var idx int
	if start < 0 {
		start = 0
		idx = line
	} else {
		idx = context
	}
	end := line + context + 1
	if line >= len(lines) {
		return nil, 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	return lines[start:end], idx
}

var trimPaths []string

// Try to trim the GOROOT or GOPATH prefix off of a filename
func trimPath(filename string) string {
	for _, prefix := range trimPaths {
		if trimmed := strings.TrimPrefix(filename, prefix); len(trimmed) < len(filename) {
			return trimmed
		}
	}
	return filename
}

func init() {
	// Collect all source directories, and make sure they
	// end in a trailing "separator"
	for _, prefix := range build.Default.SrcDirs() {
		if prefix[len(prefix)-1] != filepath.Separator {
			prefix += string(filepath.Separator)
		}
		trimPaths = append(trimPaths, prefix)
	}
}
