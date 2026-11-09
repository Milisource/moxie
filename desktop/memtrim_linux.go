//go:build linux && cgo

package main

// #include <malloc.h>
import "C"

import "runtime/debug"

func init() {
	// dav1d decodes on its own thread pool; by default glibc gives each new
	// thread its own malloc arena (up to 8×cores), and memory freed into
	// those arenas fragments and stays resident. Two arenas are plenty for
	// the C side of this app (WebKit runs in separate processes).
	C.mallopt(C.M_ARENA_MAX, 2)
}

// releaseDecoderMemory hands memory freed by a batch of image decodes back
// to the OS. AVIF covers decode through the system libavif/dav1d, whose
// frees land in glibc's per-thread malloc arenas — invisible to the Go GC —
// so a full thumbnail backfill otherwise leaves the process ~350 MB larger
// for the rest of the session. malloc_trim returns those arenas' free pages.
func releaseDecoderMemory() {
	debug.FreeOSMemory()
	C.malloc_trim(0)
}
