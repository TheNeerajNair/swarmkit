package storage

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/coreos/etcd/raft/raftpb"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/encryption"
	"github.com/stretchr/testify/require"
)

var _ SnapFactory = snapCryptor{}

var fakeSnapshotData = raftpb.Snapshot{
	Data: []byte("snapshotdata"),
	Metadata: raftpb.SnapshotMetadata{
		ConfState: raftpb.ConfState{Nodes: []uint64{3}},
		Index:     6,
		Term:      2,
	},
}

func getSnapshotFile(t *testing.T, tempdir string) string {
	var filepaths []string
	err := filepath.Walk(tempdir, func(path string, fi os.FileInfo, err error) error {
		require.NoError(t, err)
		if !fi.IsDir() {
			filepaths = append(filepaths, path)
		}
		return nil
	})
	require.NoError(t, err)
	require.Len(t, filepaths, 1)
	return filepaths[0]
}

// Snapshotter can read snapshots that are wrapped, but not encrypted
func TestSnapshotterLoadNotEncryptedSnapshot(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "snapwrap")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	ogSnap := OriginalSnap.New(tempdir)
	r := api.MaybeEncryptedRecord{
		Data: fakeSnapshotData.Data,
	}
	data, err := r.Marshal()
	require.NoError(t, err)

	emptyEncodingFakeData := fakeSnapshotData
	emptyEncodingFakeData.Data = data

	require.NoError(t, ogSnap.SaveSnap(emptyEncodingFakeData))

	c := NewSnapFactory(encryption.NoopCoder, encryption.NoopCoder)
	wrapped := c.New(tempdir)

	readSnap, err := wrapped.Load()
	require.NoError(t, err)
	require.Equal(t, fakeSnapshotData, *readSnap)
}

// If there is no decoder for a snapshot, decoding fails
func TestSnapshotterLoadNoDecoder(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "snapwrap")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	ogSnap := OriginalSnap.New(tempdir)
	r := api.MaybeEncryptedRecord{
		Data:      fakeSnapshotData.Data,
		Algorithm: meowCoder{}.Algorithm(),
	}
	data, err := r.Marshal()
	require.NoError(t, err)

	emptyEncodingFakeData := fakeSnapshotData
	emptyEncodingFakeData.Data = data

	require.NoError(t, ogSnap.SaveSnap(emptyEncodingFakeData))

	c := NewSnapFactory(encryption.NoopCoder, encryption.NoopCoder)
	wrapped := c.New(tempdir)

	_, err = wrapped.Load()
	require.Error(t, err)
}

// If decoding a snapshot fails, the error is propagated
func TestSnapshotterLoadDecodingFail(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "snapwrap")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	coder := &meowCoder{}

	ogSnap := OriginalSnap.New(tempdir)
	r := api.MaybeEncryptedRecord{
		Data:      fakeSnapshotData.Data,
		Algorithm: coder.Algorithm(),
	}
	data, err := r.Marshal()
	require.NoError(t, err)

	emptyEncodingFakeData := fakeSnapshotData
	emptyEncodingFakeData.Data = data

	require.NoError(t, ogSnap.SaveSnap(emptyEncodingFakeData))

	c := NewSnapFactory(encryption.NoopCoder, coder)
	wrapped := c.New(tempdir)

	_, err = wrapped.Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not meowcoded")
}

// The snapshot data (but not metadata or anything else) is encoded before being
// passed to the wrapped Snapshotter.
func TestSnapshotterSavesSnapshotWithEncoding(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "snapwrap")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	c := NewSnapFactory(meowCoder{}, encryption.NoopCoder)
	wrapped := c.New(tempdir)
	require.NoError(t, wrapped.SaveSnap(fakeSnapshotData))

	ogSnap := OriginalSnap.New(tempdir)
	readSnap, err := ogSnap.Load()
	require.NoError(t, err)

	r := api.MaybeEncryptedRecord{}
	require.NoError(t, r.Unmarshal(readSnap.Data))
	require.NotEqual(t, fakeSnapshotData.Data, r.Data)
	require.Equal(t, fakeSnapshotData.Metadata, readSnap.Metadata)
}

// If an encoder is passed to Snapshotter, but encoding the data fails, the
// error is propagated up
func TestSnapshotterSavesSnapshotEncodingFails(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "snapwrap")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	c := NewSnapFactory(&meowCoder{encodeFailures: map[string]struct{}{
		"snapshotdata": {},
	}}, encryption.NoopCoder)
	wrapped := c.New(tempdir)
	err = wrapped.SaveSnap(fakeSnapshotData)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to encode")

	// nothing there to read
	ogSnap := OriginalSnap.New(tempdir)
	_, err = ogSnap.Load()
	require.Error(t, err)
}

// Snapshotter can read what it wrote so long as it has the same decoder
func TestSaveAndLoad(t *testing.T) {
	coder := &meowCoder{}
	tempdir, err := ioutil.TempDir("", "waltests")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	c := NewSnapFactory(coder, coder)
	wrapped := c.New(tempdir)
	require.NoError(t, wrapped.SaveSnap(fakeSnapshotData))
	readSnap, err := wrapped.Load()
	require.NoError(t, err)
	require.Equal(t, fakeSnapshotData, *readSnap)
}
