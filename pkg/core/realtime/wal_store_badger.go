// Package realtime 提供基于 Badger 的 WAL 存储实现
package realtime

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/dgraph-io/badger/v4"
)

const (
	badgerWalKeyPrefix = "wal:"
	badgerWalMetaPrefix = "wal_meta:"
)

// badgerWalStore 基于 Badger 的 WAL 实现，支持持久化与重启恢复
type badgerWalStore struct {
	db   *badger.DB
	path string
	mu   sync.Mutex
}

// NewBadgerWalStore 创建基于 Badger 的 WAL，path 为 Badger 数据目录
func NewBadgerWalStore(path string) (WalStore, error) {
	opts := badger.DefaultOptions(path).WithLoggingLevel(badger.WARNING)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("打开 Badger WAL: %w", err)
	}
	return &badgerWalStore{db: db, path: path}, nil
}

func (s *badgerWalStore) key(instanceID string, seqID int64) []byte {
	// key: wal:instanceID + 8 字节大端 seqID，便于按实例+顺序迭代
	b := make([]byte, 0, len(badgerWalKeyPrefix)+len(instanceID)+8)
	b = append(b, badgerWalKeyPrefix...)
	b = append(b, instanceID...)
	b = append(b, '\x00')
	seqBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuf, uint64(seqID))
	b = append(b, seqBuf...)
	return b
}

func (s *badgerWalStore) prefix(instanceID string) []byte {
	b := make([]byte, 0, len(badgerWalKeyPrefix)+len(instanceID)+1)
	b = append(b, badgerWalKeyPrefix...)
	b = append(b, instanceID...)
	b = append(b, '\x00')
	return b
}

func (s *badgerWalStore) Append(ctx context.Context, rec *WalRecord) error {
	key := s.key(rec.InstanceID, rec.SequenceID)
	val, err := encodeWalRecord(rec)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		if err == nil {
			return nil // 已存在则幂等
		}
		if err != badger.ErrKeyNotFound {
			return err
		}
		return txn.Set(key, val)
	})
}

func (s *badgerWalStore) MarkAcked(ctx context.Context, instanceID string, seqID int64) error {
	key := s.key(instanceID, seqID)
	return s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}
			return err
		}
		val, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		rec, err := decodeWalRecord(val)
		if err != nil {
			return err
		}
		rec.Acked = true
		newVal, err := encodeWalRecord(rec)
		if err != nil {
			return err
		}
		return txn.Set(key, newVal)
	})
}

func (s *badgerWalStore) IterateUnacked(ctx context.Context, instanceID string, fn func(*WalRecord) error) error {
	prefix := s.prefix(instanceID)
	return s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 10
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			item := it.Item()
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			rec, err := decodeWalRecord(val)
			if err != nil {
				return err
			}
			if rec.Acked {
				continue
			}
			if err := fn(rec); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *badgerWalStore) GC(ctx context.Context, instanceID string) error {
	prefix := s.prefix(instanceID)
	var toDelete [][]byte
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			rec, err := decodeWalRecord(val)
			if err != nil {
				continue
			}
			if rec.Acked {
				key := item.KeyCopy(nil)
				toDelete = append(toDelete, key)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(toDelete) == 0 {
		return nil
	}
	return s.db.Update(func(txn *badger.Txn) error {
		for _, key := range toDelete {
			if err := txn.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *badgerWalStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// 简单二进制编码：8 字节 instanceID 长度 + instanceID + 8 字节 seqID + 1 字节 acked + 4 字节 data 长度 + data
func encodeWalRecord(rec *WalRecord) ([]byte, error) {
	il := len(rec.InstanceID)
	dl := len(rec.Data)
	b := make([]byte, 0, 8+il+8+1+4+dl)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(il))
	b = append(b, buf...)
	b = append(b, rec.InstanceID...)
	binary.BigEndian.PutUint64(buf, uint64(rec.SequenceID))
	b = append(b, buf...)
	if rec.Acked {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	binary.BigEndian.PutUint32(buf[:4], uint32(dl))
	b = append(b, buf[:4]...)
	b = append(b, rec.Data...)
	return b, nil
}

func decodeWalRecord(b []byte) (*WalRecord, error) {
	if len(b) < 8+8+1+4 {
		return nil, fmt.Errorf("wal record too short")
	}
	il := binary.BigEndian.Uint64(b[:8])
	b = b[8:]
	if uint64(len(b)) < il+8+1+4 {
		return nil, fmt.Errorf("wal record truncated")
	}
	rec := &WalRecord{
		InstanceID: string(b[:il]),
		SequenceID: int64(binary.BigEndian.Uint64(b[il : il+8])),
		Acked:      b[il+8] != 0,
	}
	b = b[il+8+1:]
	dl := binary.BigEndian.Uint32(b[:4])
	b = b[4:]
	if uint32(len(b)) < dl {
		return nil, fmt.Errorf("wal record data truncated")
	}
	rec.Data = make([]byte, dl)
	copy(rec.Data, b[:dl])
	return rec, nil
}
