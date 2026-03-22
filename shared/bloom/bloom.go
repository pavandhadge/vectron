// bloom.go - Bloom filter implementation for fast negative lookups
//
// A Bloom filter is a space-efficient probabilistic data structure that is used
// to test whether an element is a member of a set. False positive matches are
// possible, but false negatives are not – in other words, a query returns either
// "possibly in set" or "definitely not in set".

package bloom

import (
	"hash/fnv"
	"math"
	"sync"
)

// Filter is a Bloom filter implementation
type Filter struct {
	bits []uint64
	k    uint32 // number of hash functions
	m    uint32 // size of bit array
	n    uint32 // number of elements added
	mu   sync.RWMutex
}

// New creates a new Bloom filter with the given capacity and false positive rate
func New(capacity int, falsePositiveRate float64) *Filter {
	if capacity <= 0 {
		capacity = 1000
	}
	if falsePositiveRate <= 0 || falsePositiveRate >= 1 {
		falsePositiveRate = 0.01
	}

	// Calculate optimal size and number of hash functions
	m := optimalM(capacity, falsePositiveRate)
	k := optimalK(capacity, m)

	return &Filter{
		bits: make([]uint64, (m+63)/64),
		k:    k,
		m:    m,
		n:    0,
	}
}

// optimalM calculates the optimal size of the bit array
func optimalM(n int, p float64) uint32 {
	return uint32(math.Ceil(-float64(n) * math.Log(p) / (math.Log(2) * math.Log(2))))
}

// optimalK calculates the optimal number of hash functions
func optimalK(n int, m uint32) uint32 {
	k := uint32(math.Ceil(float64(m) / float64(n) * math.Log(2)))
	if k < 1 {
		k = 1
	}
	if k > 20 {
		k = 20
	}
	return k
}

// Add adds an element to the Bloom filter
func (f *Filter) Add(data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()

	h1, h2 := f.hash(data)
	for i := uint32(0); i < f.k; i++ {
		idx := (h1 + uint64(i)*h2) % uint64(f.m)
		f.bits[idx/64] |= 1 << (idx % 64)
	}
	f.n++
}

// AddString adds a string element to the Bloom filter
func (f *Filter) AddString(s string) {
	f.Add([]byte(s))
}

// Contains tests if an element might be in the set
// Returns true if the element might be in the set (could be false positive)
// Returns false if the element is definitely not in the set
func (f *Filter) Contains(data []byte) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	h1, h2 := f.hash(data)
	for i := uint32(0); i < f.k; i++ {
		idx := (h1 + uint64(i)*h2) % uint64(f.m)
		if f.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

// ContainsString tests if a string element might be in the set
func (f *Filter) ContainsString(s string) bool {
	return f.Contains([]byte(s))
}

// hash computes two hash values for double hashing
func (f *Filter) hash(data []byte) (uint64, uint64) {
	h := fnv.New64a()
	h.Write(data)
	h1 := h.Sum64()

	h.Reset()
	h.Write(data)
	h.Write([]byte{1}) // Different seed for second hash
	h2 := h.Sum64()
	if h2 == 0 {
		h2 = 1
	}

	return h1, h2
}

// Count returns the number of elements added to the filter
func (f *Filter) Count() uint32 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.n
}

// Reset clears the filter
func (f *Filter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.bits {
		f.bits[i] = 0
	}
	f.n = 0
}

// EstimatedFalsePositiveRate returns the estimated false positive rate
func (f *Filter) EstimatedFalsePositiveRate() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.n == 0 {
		return 0
	}
	return math.Pow(1-math.Exp(-float64(f.k)*float64(f.n)/float64(f.m)), float64(f.k))
}
