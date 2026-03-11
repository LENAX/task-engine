// Package realtime 提供 WAL（Write-Ahead Log）存储接口与实现，用于进程内 at-least-once 语义
package realtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// WalRecord WAL 记录
type WalRecord struct {
	InstanceID string `json:"instance_id"`
	SequenceID int64  `json:"sequence_id"`
	Data       []byte `json:"data"`
	Acked      bool   `json:"acked"`
}

// WalStore WAL 存储接口
type WalStore interface {
	Append(ctx context.Context, rec *WalRecord) error
	MarkAcked(ctx context.Context, instanceID string, seqID int64) error
	IterateUnacked(ctx context.Context, instanceID string, fn func(*WalRecord) error) error
	GC(ctx context.Context, instanceID string) error
	Close() error
}

// memoryWalStore 内存 WAL 实现（单进程、重启丢失，适合测试或无需持久化场景）
type memoryWalStore struct {
	mu      sync.RWMutex
	records map[string]*WalRecord // key = instanceID + "\x00" + seqID
	order   map[string][]int64    // instanceID -> 有序 seqID 列表
}

// NewMemoryWalStore 创建内存 WAL
func NewMemoryWalStore() WalStore {
	return &memoryWalStore{
		records: make(map[string]*WalRecord),
		order:   make(map[string][]int64),
	}
}

func (s *memoryWalStore) key(instanceID string, seqID int64) string {
	return instanceID + "\x00" + strconv.FormatInt(seqID, 10)
}

func (s *memoryWalStore) Append(ctx context.Context, rec *WalRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(rec.InstanceID, rec.SequenceID)
	if _, exists := s.records[key]; exists {
		return nil
	}
	r := *rec
	s.records[key] = &r
	s.order[rec.InstanceID] = append(s.order[rec.InstanceID], rec.SequenceID)
	return nil
}

func (s *memoryWalStore) MarkAcked(ctx context.Context, instanceID string, seqID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(instanceID, seqID)
	if r, ok := s.records[key]; ok {
		r.Acked = true
	}
	return nil
}

func (s *memoryWalStore) IterateUnacked(ctx context.Context, instanceID string, fn func(*WalRecord) error) error {
	s.mu.RLock()
	seqs := make([]int64, len(s.order[instanceID]))
	copy(seqs, s.order[instanceID])
	s.mu.RUnlock()
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for _, seqID := range seqs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		s.mu.RLock()
		r, ok := s.records[s.key(instanceID, seqID)]
		if !ok || r.Acked {
			s.mu.RUnlock()
			continue
		}
		rec := *r
		s.mu.RUnlock()
		if err := fn(&rec); err != nil {
			return err
		}
	}
	return nil
}

func (s *memoryWalStore) GC(ctx context.Context, instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var newOrder []int64
	for _, seqID := range s.order[instanceID] {
		key := s.key(instanceID, seqID)
		if r, ok := s.records[key]; ok && r.Acked {
			delete(s.records, key)
		} else {
			newOrder = append(newOrder, seqID)
		}
	}
	s.order[instanceID] = newOrder
	return nil
}

func (s *memoryWalStore) Close() error { return nil }

// fileWalStore 基于目录文件的 WAL：baseDir/instanceID/seqID 为文件，内容为 Data；acked 通过 .acked 后缀表示
type fileWalStore struct {
	baseDir string
	mu      sync.Mutex
}

// NewFileWalStore 创建基于文件的 WAL，baseDir 为根目录
func NewFileWalStore(baseDir string) (WalStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("创建 WAL 目录失败: %w", err)
	}
	return &fileWalStore{baseDir: baseDir}, nil
}

func (f *fileWalStore) instanceDir(instanceID string) string {
	return filepath.Join(f.baseDir, instanceID)
}

func (f *fileWalStore) Append(ctx context.Context, rec *WalRecord) error {
	dir := f.instanceDir(rec.InstanceID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, strconv.FormatInt(rec.SequenceID, 10))
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, rec.Data, 0644)
}

func (f *fileWalStore) MarkAcked(ctx context.Context, instanceID string, seqID int64) error {
	path := filepath.Join(f.instanceDir(instanceID), strconv.FormatInt(seqID, 10))
	ackedPath := path + ".acked"
	return os.WriteFile(ackedPath, nil, 0644)
}

func (f *fileWalStore) IterateUnacked(ctx context.Context, instanceID string, fn func(*WalRecord) error) error {
	dir := f.instanceDir(instanceID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var seqIDs []int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if len(e.Name()) > 0 && e.Name()[0] == '.' {
			continue
		}
		if filepath.Ext(e.Name()) == ".acked" {
			continue
		}
		n, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name()+".acked")); err == nil {
			continue
		}
		seqIDs = append(seqIDs, n)
	}
	sort.Slice(seqIDs, func(i, j int) bool { return seqIDs[i] < seqIDs[j] })
	for _, seqID := range seqIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		path := filepath.Join(dir, strconv.FormatInt(seqID, 10))
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if err := fn(&WalRecord{InstanceID: instanceID, SequenceID: seqID, Data: data}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fileWalStore) GC(ctx context.Context, instanceID string) error {
	dir := f.instanceDir(instanceID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".acked") {
			base := name[:len(name)-len(".acked")]
			_ = os.Remove(filepath.Join(dir, base))
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}

func (f *fileWalStore) Close() error { return nil }
