package runtimeutil

import (
	"log"
	"os"
	"runtime"
	"strconv"
)

// ConfigureGOMAXPROCS sets GOMAXPROCS based on VECTRON_GOMAXPROCS or NumCPU.
// Returns the value set (or current if unchanged).
func ConfigureGOMAXPROCS(service string) int {
	if v := os.Getenv("VECTRON_GOMAXPROCS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			prev := runtime.GOMAXPROCS(n)
			log.Printf("%s: GOMAXPROCS set to %d (was %d)", service, n, prev)
			return n
		}
	}
	n := runtime.NumCPU()
	if n <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	prev := runtime.GOMAXPROCS(n)
	log.Printf("%s: GOMAXPROCS set to %d (was %d)", service, n, prev)
	return n
}
