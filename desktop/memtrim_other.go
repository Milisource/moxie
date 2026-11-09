//go:build !linux || !cgo

package main

import "runtime/debug"

// releaseDecoderMemory returns freed heap to the OS (see memtrim_linux.go).
func releaseDecoderMemory() { debug.FreeOSMemory() }
