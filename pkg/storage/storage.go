package storage

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/korjavin/whatsfordinner/pkg/logger"
)

// Store represents a BadgerDB storage instance
type Store struct {
	db *badger.DB
}

// New creates a new BadgerDB storage instance
func New(dataDir string) (*Store, error) {
	// Ensure the data directory exists
	absPath, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Open the Badger database
	opts := badger.DefaultOptions(absPath)
	opts.Logger = nil // Disable Badger's internal logger

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open BadgerDB: %w", err)
	}

	logger.Global.Info("BadgerDB opened at %s", absPath)
	return &Store{db: db}, nil
}

// Close closes the BadgerDB database
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Set stores a value for a key
func (s *Store) Set(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})
}

// Get retrieves a value for a key
func (s *Store) Get(key string, value interface{}) error {
	var data []byte
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			data = append([]byte{}, val...)
			return nil
		})
	})

	if err != nil {
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("key not found: %s", key)
		}
		return fmt.Errorf("failed to get value: %w", err)
	}

	return json.Unmarshal(data, value)
}

// Delete removes a key from the database
func (s *Store) Delete(key string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}

// List returns all keys with a given prefix
func (s *Store) List(prefix string) ([]string, error) {
	var keys []string
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		prefixBytes := []byte(prefix)
		for it.Seek(prefixBytes); it.ValidForPrefix(prefixBytes); it.Next() {
			item := it.Item()
			key := string(item.Key())
			keys = append(keys, key)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	return keys, nil
}

// RunGC runs garbage collection on the database
func (s *Store) RunGC() error {
	return s.db.RunValueLogGC(0.5)
}

// StartGCRoutine starts a goroutine that periodically runs garbage collection
func (s *Store) StartGCRoutine(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			err := s.RunGC()
			if err != nil {
				// Only log when GC actually did something
				if err != badger.ErrNoRewrite {
					logger.Global.Error("BadgerDB GC error: %v", err)
				}
			}
		}
	}()
	logger.Global.Info("Started BadgerDB GC routine with interval %v", interval)
}
