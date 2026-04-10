package segment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/cockroachdb/pebble"
)

const (
	segManifestKey     = "seg_manifest"
	segMetaPrefix      = "seg_meta_"
	segDocPrefix       = "seg_doc_"
	segDelPrefix       = "seg_del_"
	segTombstonePrefix = "seg_tombstone_"
)

func ManifestKey() []byte {
	return []byte(segManifestKey)
}

func SegmentMetaKey(id SegmentID) []byte {
	return []byte(segMetaPrefix + string(id))
}

func SegmentDeleteKey(docID string) []byte {
	return []byte(segDelPrefix + docID)
}

func ParseSegmentMetaKey(key []byte) (SegmentID, bool) {
	prefix := []byte(segMetaPrefix)
	if len(key) <= len(prefix) {
		return "", false
	}
	if string(key[:len(prefix)]) != segMetaPrefix {
		return "", false
	}
	return SegmentID(string(key[len(prefix):])), true
}

type ManifestStore struct {
	db      *pebble.DB
	shardID uint64
}

func NewManifestStore(db *pebble.DB, shardID uint64) *ManifestStore {
	return &ManifestStore{db: db, shardID: shardID}
}

func (s *ManifestStore) key(base []byte) []byte {
	prefix := []byte(fmt.Sprintf("seg_s%d_", s.shardID))
	return append(prefix, base...)
}

func (s *ManifestStore) Load() (*ShardManifest, error) {
	data, closer, err := s.db.Get(s.key(ManifestKey()))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	defer closer.Close()

	var manifest ShardManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (s *ManifestStore) Save(manifest *ShardManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return s.db.Set(s.key(ManifestKey()), data, nil)
}

func (s *ManifestStore) LoadSegmentMeta(id SegmentID) (*SegmentMeta, error) {
	data, closer, err := s.db.Get(s.key(SegmentMetaKey(id)))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	defer closer.Close()

	var meta SegmentMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *ManifestStore) SaveSegmentMeta(meta *SegmentMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return s.db.Set(s.key(SegmentMetaKey(meta.ID)), data, nil)
}

func (s *ManifestStore) DeleteSegmentMeta(id SegmentID) error {
	return s.db.Delete(s.key(SegmentMetaKey(id)), nil)
}

func (s *ManifestStore) ListSegmentMetas() ([]SegmentMeta, error) {
	prefix := s.key([]byte(segMetaPrefix))
	iter := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixPrefixSuccessor(prefix),
	})
	defer iter.Close()

	var metas []SegmentMeta
	for iter.First(); iter.Valid(); iter.Next() {
		var meta SegmentMeta
		if err := json.Unmarshal(iter.Value(), &meta); err != nil {
			continue
		}
		metas = append(metas, meta)
	}
	return metas, iter.Error()
}

func SegmentDocKey(shardID uint64, segID SegmentID, docID string) []byte {
	return []byte(fmt.Sprintf("%s%d_%s_%s", segDocPrefix, shardID, segID, docID))
}

func SegmentDocPrefix(shardID uint64, segID SegmentID) []byte {
	return []byte(fmt.Sprintf("%s%d_%s_", segDocPrefix, shardID, segID))
}

func ParseSegmentDocKey(key []byte) (SegmentID, string, bool) {
	if !bytes.HasPrefix(key, []byte(segDocPrefix)) {
		return "", "", false
	}
	rest := string(key[len(segDocPrefix):])
	first := stringsIndexByte(rest, '_')
	second := stringsIndexByte(rest[first+1:], '_')
	if first <= 0 || second < 0 {
		return "", "", false
	}
	second += first + 1
	return SegmentID(rest[first+1 : second]), rest[second+1:], true
}

func SegmentTombstoneKey(shardID uint64, docID string, epoch int64) []byte {
	return []byte(fmt.Sprintf("%s%d_%s_%d", segTombstonePrefix, shardID, docID, epoch))
}

func SegmentTombstonePrefix(shardID uint64) []byte {
	return []byte(fmt.Sprintf("%s%d_", segTombstonePrefix, shardID))
}

func ParseSegmentTombstoneKey(key []byte) (uint64, string, int64, bool) {
	if !bytes.HasPrefix(key, []byte(segTombstonePrefix)) {
		return 0, "", 0, false
	}
	rest := string(key[len(segTombstonePrefix):])
	first := stringsIndexByte(rest, '_')
	last := stringsLastIndexByte(rest, '_')
	if first <= 0 || last <= first {
		return 0, "", 0, false
	}
	shardID, err := strconv.ParseUint(rest[:first], 10, 64)
	if err != nil {
		return 0, "", 0, false
	}
	epoch, err := strconv.ParseInt(rest[last+1:], 10, 64)
	if err != nil {
		return 0, "", 0, false
	}
	return shardID, rest[first+1 : last], epoch, true
}

func stringsIndexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func stringsLastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func prefixPrefixSuccessor(prefix []byte) []byte {
	for i := len(prefix) - 1; i >= 0; i-- {
		if prefix[i] < 0xff {
			result := make([]byte, len(prefix))
			copy(result, prefix)
			result[i]++
			return result
		}
	}
	return nil
}
