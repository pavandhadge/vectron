package storage

import (
	"errors"

	"github.com/cockroachdb/pebble"
)

// Init initializes and opens a PebbleDB instance at the given path.
func (r *PebbleDB) Init(path string, opts *Options) error {
	dbOpts := &pebble.Options{}

	if opts != nil {
		if opts.MaxOpenFiles > 0 {
			dbOpts.MaxOpenFiles = opts.MaxOpenFiles
		}
		if opts.WriteBufferSize > 0 {
			dbOpts.MemTableSize = uint64(opts.WriteBufferSize)
		}
		if opts.CacheSize > 0 {
			cache := pebble.NewCache(opts.CacheSize)
			dbOpts.Cache = cache
		}
	}

	var err error
	r.db, err = pebble.Open(path, dbOpts)
	if err != nil {
		return err
	}

	r.iterOpts = &pebble.IterOptions{}
	r.writeOpts = &pebble.WriteOptions{}

	return nil
}

// Close closes the PebbleDB instance.
func (r *PebbleDB) Close() error {
	if r.db != nil {
		r.db.Close()
		return nil
	}
	return errors.New("db not initialized")
}

// Status returns the status of the database.
func (r *PebbleDB) Status() (string, error) {
	if r.db == nil {
		return "", errors.New("db not initialized")
	}
	// PebbleDB.stats is not a valid property in Pebble.
	// We can use r.db.Metrics().String() for detailed status information.
	return r.db.Metrics().String(), nil
}

// Compact compacts the entire database.
func (r *PebbleDB) Compact() error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Compact(nil, nil, false)
}

// Flush flushes the database writes to disk.
func (r *PebbleDB) Flush() error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Flush()
}

// Backup creates a backup of the database.
func (r *PebbleDB) Backup(path string) error {
	return errors.New("backup not implemented")
}

// Restore restores a backup of the database.
func (r *PebbleDB) Restore(backupPath string) error {
	return errors.New("restore not implemented")
}
