// Copyright 2015 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package session

import (
	"context"
	"crypto/rand"
	"io"
	"io/fs"
	"log"
	"os"
	"time"

	"github.com/hugelgupf/p9/p9"
	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseops"
	"github.com/jacobsa/fuse/fuseutil"
	"github.com/jacobsa/syncutil"
)

// Create a file system that issues cacheable responses according to the
// following rules:
//
//   - LookUpInodeResponse.Entry.EntryExpiration is set according to
//     lookupEntryTimeout.
//
//   - GetInodeAttributesResponse.AttributesExpiration is set according to
//     getattrTimeout.
//
//   - Nothing else is marked cacheable. (In particular, the attributes
//     returned by LookUpInode are not cacheable.)
func NewP9FS(cl *p9.Client, lookupEntryTimeout time.Duration, getattrTimeout time.Duration) (fuse.Server, *P9FS, error) {
	cfs := &P9FS{
		cl:                 cl,
		lookupEntryTimeout: lookupEntryTimeout,
		getattrTimeout:     getattrTimeout,
		mtime:              time.Now(),
		inMap:              make(map[fuseops.InodeID]entry),
	}

	return fuseutil.NewFileSystemServer(cfs), cfs, nil
}

type entry struct {
	fid     p9.File
	root    bool
	QID     p9.QID
	inumber uint64
}

type P9FS struct {
	/////////////////////////
	// Constant data
	/////////////////////////

	lookupEntryTimeout time.Duration
	getattrTimeout     time.Duration
	cl                 *p9.Client

	/////////////////////////
	// Mutable state
	/////////////////////////

	mu syncutil.InvariantMutex

	// GUARDED_BY(mu)
	keepPageCache bool
	mtime         time.Time
	inMap         map[fuseops.InodeID]entry
}

var _ fuseutil.FileSystem = &P9FS{}

////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////

// LOCKS_REQUIRED(fs.mu)
func (fs *P9FS) rootAttrs() fuseops.InodeAttributes {
	return fuseops.InodeAttributes{
		Mode:  os.ModeDir | 0777,
		Mtime: fs.mtime,
	}
}

////////////////////////////////////////////////////////////////////////
// Public interface
////////////////////////////////////////////////////////////////////////

// LOCKS_EXCLUDED(fs.mu)
func (fs *P9FS) SetMtime(mtime time.Time) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.mtime = mtime
}

// LOCKS_EXCLUDED(fs.mu)
func (fs *P9FS) SetKeepCache(keep bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.keepPageCache = keep
}

////////////////////////////////////////////////////////////////////////
// FileSystem methods
////////////////////////////////////////////////////////////////////////

func (fs *P9FS) StatFS(ctx context.Context, op *fuseops.StatFSOp) error {
	return nil
}

// LOCKS_EXCLUDED(fs.mu)
func (fs *P9FS) LookUpInode(ctx context.Context, op *fuseops.LookUpInodeOp) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Find the ID and attributes.
	var id fuseops.InodeID
	var attrs fuseops.InodeAttributes

	return fuse.ENOENT
	// Fill in the response.
	op.Entry.Child = id
	op.Entry.Attributes = attrs
	op.Entry.EntryExpiration = time.Now().Add(fs.lookupEntryTimeout)

	return nil
}

// LOCKS_EXCLUDED(fs.mu)
func (p9fs *P9FS) GetInodeAttributes(ctx context.Context, op *fuseops.GetInodeAttributesOp) error {
	p9fs.mu.Lock()
	defer p9fs.mu.Unlock()

	// Figure out which inode the request is for.
	in := op.Inode
	cl, ok := p9fs.inMap[in]
	if !ok {
		panic("NO file")
		return os.ErrNotExist
	}

	v("GetInodeAttributes for in %d cl %v", in, cl)
	q, _, a, err := cl.fid.GetAttr(p9.AttrMaskAll)
	if err != nil {
		panic("bad getattr")
		v("cl.GetAttr: %v", err)
		return err
	}

	var dir fs.FileMode
	if q.Type&p9.TypeDir == p9.TypeDir {
		dir = os.ModeDir
	}
	attrs := fuseops.InodeAttributes{
		Size:   a.Size,
		Nlink:  uint32(a.NLink),
		Mode:   dir | fs.FileMode(a.Mode),
		Atime:  time.Now(),
		Mtime:  time.Now(),
		Ctime:  time.Now(),
		Crtime: time.Now(),
		Uid:    uint32(a.UID),
		Gid:    uint32(a.GID),
	}
	op.Attributes = attrs
	op.AttributesExpiration = time.Now().Add(p9fs.getattrTimeout)
	v("GetInodeAttributes: OK")
	// NOTE: if you get an EIO from this, it's usually b/c the ModeDir bit
	// is wrong.
	log.Printf("attr %v", attrs)
	return nil
}

func (fs *P9FS) OpenDir(ctx context.Context, op *fuseops.OpenDirOp) error {
	return nil
}

func (fs *P9FS) OpenFile(ctx context.Context, op *fuseops.OpenFileOp) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	op.KeepPageCache = fs.keepPageCache

	return nil
}

func (fs *P9FS) ReadFile(ctx context.Context, op *fuseops.ReadFileOp) error {
	var err error
	op.BytesRead, err = io.ReadFull(rand.Reader, op.Dst)
	return err
}

// The fuse package says to embed a fuseutil.NotImplementedFileSystem in your struct
// to catch all the stuff you don't implement. That way lies madness, we've tried
// it, it's basically undebuggable. So we put all these not implemented bits here.
// A FileSystem that responds to all ops with fuse.ENOSYS. Embed this in your
// struct to inherit default implementations for the methods you don't care
// about, ensuring your struct will continue to implement FileSystem even as
// new methods are added.
func (fs *P9FS) SetInodeAttributes(ctx context.Context, op *fuseops.SetInodeAttributesOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) ForgetInode(ctx context.Context, op *fuseops.ForgetInodeOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) BatchForget(ctx context.Context, op *fuseops.BatchForgetOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) MkDir(ctx context.Context, op *fuseops.MkDirOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) MkNode(ctx context.Context, op *fuseops.MkNodeOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) CreateFile(ctx context.Context, op *fuseops.CreateFileOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) CreateSymlink(ctx context.Context, op *fuseops.CreateSymlinkOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) CreateLink(ctx context.Context, op *fuseops.CreateLinkOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) Rename(ctx context.Context, op *fuseops.RenameOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) RmDir(ctx context.Context, op *fuseops.RmDirOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) Unlink(ctx context.Context, op *fuseops.UnlinkOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) ReadDir(ctx context.Context, op *fuseops.ReadDirOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) ReleaseDirHandle(ctx context.Context, op *fuseops.ReleaseDirHandleOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) WriteFile(ctx context.Context, op *fuseops.WriteFileOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) SyncFile(ctx context.Context, op *fuseops.SyncFileOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) FlushFile(ctx context.Context, op *fuseops.FlushFileOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) ReleaseFileHandle(ctx context.Context, op *fuseops.ReleaseFileHandleOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) ReadSymlink(ctx context.Context, op *fuseops.ReadSymlinkOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) RemoveXattr(ctx context.Context, op *fuseops.RemoveXattrOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) GetXattr(ctx context.Context, op *fuseops.GetXattrOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) ListXattr(ctx context.Context, op *fuseops.ListXattrOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) SetXattr(ctx context.Context, op *fuseops.SetXattrOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) Fallocate(ctx context.Context, op *fuseops.FallocateOp) error {
	return fuse.ENOSYS
}

func (fs *P9FS) Destroy() {
}
