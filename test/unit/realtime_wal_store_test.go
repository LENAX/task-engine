package unit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LENAX/task-engine/pkg/core/realtime"
)

func TestMemoryWalStore_AppendAndIterateUnacked(t *testing.T) {
	ctx := context.Background()
	store := realtime.NewMemoryWalStore()
	defer store.Close()

	rec := &realtime.WalRecord{
		InstanceID: "inst1",
		SequenceID: 1,
		Data:       []byte(`{"x":1}`),
		Acked:      false,
	}
	err := store.Append(ctx, rec)
	require.NoError(t, err)

	var seen int
	err = store.IterateUnacked(ctx, "inst1", func(r *realtime.WalRecord) error {
		seen++
		assert.Equal(t, int64(1), r.SequenceID)
		assert.Equal(t, []byte(`{"x":1}`), r.Data)
		assert.False(t, r.Acked)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, seen)
}

func TestMemoryWalStore_MarkAckedAndGC(t *testing.T) {
	ctx := context.Background()
	store := realtime.NewMemoryWalStore()
	defer store.Close()

	require.NoError(t, store.Append(ctx, &realtime.WalRecord{InstanceID: "inst1", SequenceID: 1, Data: []byte("a"), Acked: false}))
	require.NoError(t, store.Append(ctx, &realtime.WalRecord{InstanceID: "inst1", SequenceID: 2, Data: []byte("b"), Acked: false}))

	require.NoError(t, store.MarkAcked(ctx, "inst1", 1))

	var count int
	require.NoError(t, store.IterateUnacked(ctx, "inst1", func(*realtime.WalRecord) error {
		count++
		return nil
	}))
	assert.Equal(t, 1, count)

	require.NoError(t, store.GC(ctx, "inst1"))
	count = 0
	require.NoError(t, store.IterateUnacked(ctx, "inst1", func(*realtime.WalRecord) error {
		count++
		return nil
	}))
	assert.Equal(t, 1, count)
}

func TestBadgerWalStore_AppendMarkAckedIterateGC(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "badger_wal")
	store, err := realtime.NewBadgerWalStore(dir)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	require.NoError(t, store.Append(ctx, &realtime.WalRecord{InstanceID: "i1", SequenceID: 1, Data: []byte("one")}))
	require.NoError(t, store.Append(ctx, &realtime.WalRecord{InstanceID: "i1", SequenceID: 2, Data: []byte("two")}))

	var seqs []int64
	require.NoError(t, store.IterateUnacked(ctx, "i1", func(r *realtime.WalRecord) error {
		seqs = append(seqs, r.SequenceID)
		return nil
	}))
	assert.Equal(t, []int64{1, 2}, seqs)

	require.NoError(t, store.MarkAcked(ctx, "i1", 1))
	seqs = nil
	require.NoError(t, store.IterateUnacked(ctx, "i1", func(r *realtime.WalRecord) error {
		seqs = append(seqs, r.SequenceID)
		return nil
	}))
	assert.Equal(t, []int64{2}, seqs)

	require.NoError(t, store.GC(ctx, "i1"))
	seqs = nil
	require.NoError(t, store.IterateUnacked(ctx, "i1", func(r *realtime.WalRecord) error {
		seqs = append(seqs, r.SequenceID)
		return nil
	}))
	assert.Equal(t, []int64{2}, seqs)
}

func TestBadgerWalStore_OpenAndClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "badger_close")
	store, err := realtime.NewBadgerWalStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.Close())
}

func TestBadgerWalStore_PersistsAcrossOpenClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "badger_persist")
	ctx := context.Background()

	{
		store, err := realtime.NewBadgerWalStore(dir)
		require.NoError(t, err)
		require.NoError(t, store.Append(ctx, &realtime.WalRecord{InstanceID: "p1", SequenceID: 42, Data: []byte("persisted")}))
		require.NoError(t, store.Close())
	}

	{
		store, err := realtime.NewBadgerWalStore(dir)
		require.NoError(t, err)
		defer store.Close()
		var got []byte
		require.NoError(t, store.IterateUnacked(ctx, "p1", func(r *realtime.WalRecord) error {
			got = r.Data
			return nil
		}))
		assert.Equal(t, []byte("persisted"), got)
	}
}
