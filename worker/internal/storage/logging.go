package storage

import (
	"log"
	"os"
	"strconv"
	"sync/atomic"
)

var (
	storageDebugLogs         = os.Getenv("STORAGE_DEBUG_LOGS") == "1"
	storageLogSampleEvery    uint64 = 100
	storageLogSampleCounter  uint64
)

func init() {
	if v := os.Getenv("STORAGE_LOG_SAMPLE_EVERY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n <= 1 {
				storageLogSampleEvery = 1
			} else {
				storageLogSampleEvery = uint64(n)
			}
		}
	}
}

func shouldLogStorageHotPath() bool {
	if storageDebugLogs {
		return true
	}
	if storageLogSampleEvery <= 1 {
		return true
	}
	return atomic.AddUint64(&storageLogSampleCounter, 1)%storageLogSampleEvery == 0
}

func logStoragef(format string, args ...interface{}) {
	if shouldLogStorageHotPath() {
		log.Printf(format, args...)
	}
}
