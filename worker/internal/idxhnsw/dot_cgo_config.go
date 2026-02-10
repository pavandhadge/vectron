package idxhnsw

import (
	"os"
	"strconv"
)

var dotCgoMinDim = func() int {
	v := os.Getenv("VECTRON_DOT_CGO_MIN_DIM")
	if v == "" {
		return 128
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 128
	}
	return n
}()

var dotCgoBatchEnabled = os.Getenv("VECTRON_DOT_CGO_BATCH") == "1"

var dotCgoBatchMin = func() int {
	v := os.Getenv("VECTRON_DOT_CGO_BATCH_MIN")
	if v == "" {
		return 64
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 2 {
		return 64
	}
	return n
}()
