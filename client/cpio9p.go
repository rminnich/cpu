// Copyright 2018 The gVisor Authors.
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

package client

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/hugelgupf/p9/p9"
	"github.com/u-root/u-root/pkg/cpio"
)

// CPIO9P is a p9.Attacher.
type CPIO9P struct {
	p9.DefaultWalkGetAttr

	file *os.File
	rr   cpio.RecordReader
	m    map[string]uint64
	recs []cpio.Record
}

// CPIO9PFile defines a FID.
// It kind of sucks because it has a pointer
// for every FID. Luckily they go away when clunked.
type CPIO9PFID struct {
	p9.DefaultWalkGetAttr

	fs   *CPIO9P
	path uint64
}

// NewCPIO9P returns a CPIO9P, properly initialized.
func NewCPIO9P(c string) (*CPIO9P, error) {
	f, err := os.Open(c)
	if err != nil {
		return nil, err
	}

	archive, err := cpio.Format("newc")
	if err != nil {
		return nil, err
	}

	rr, err := archive.NewFileReader(f)
	if err != nil {
		return nil, err
	}

	recs, err := cpio.ReadAllRecords(rr)
	if len(recs) == 0 {
		return nil, fmt.Errorf("No records: %w", os.ErrInvalid)
	}

	if err != nil {
		return nil, err
	}

	m := map[string]uint64{}
	for i, r := range recs {
		m[r.Info.Name] = uint64(i)
	}

	return &CPIO9P{file: f, rr: rr, recs: recs, m: m}, nil
}

// Attach implements p9.Attacher.Attach.
// Only works for root.
func (s *CPIO9P) Attach() (p9.File, error) {
	return &CPIO9PFID{fs: s, path: 0}, nil
}

var (
	_ p9.File     = &CPIO9PFID{}
	_ p9.Attacher = &CPIO9P{}
)

func (l *CPIO9PFID) rec() (*cpio.Record, error) {
	if int(l.path) > len(l.fs.recs) {
		return nil, os.ErrNotExist
	}
	v("rec for %v is %v", l, l.fs.recs[l.path])
	return &l.fs.recs[l.path], nil
}

// info constructs a QID for this file.
func (l *CPIO9PFID) info() (p9.QID, *cpio.Info, error) {
	var qid p9.QID

	r, err := l.rec()
	if err != nil {
		return qid, nil, err
	}

	fi := r.Info
	// Construct the QID type.
	//qid.Type = p9.ModeFromOS(fi.Mode).QIDType()

	// Save the path from the Ino.
	qid.Path = l.path
	return qid, &fi, nil
}

// Walk implements p9.File.Walk.
func (l *CPIO9PFID) Walk(names []string) ([]p9.QID, p9.File, error) {
	r, err := l.rec()
	if err != nil {
		return nil, nil, err
	}
	verbose("starting record for %v is %v", l, r)
	var qids []p9.QID
	last := &CPIO9PFID{path: l.path, fs: l.fs}
	// If the names are empty we return info for l
	// An extra stat is never hurtful; all servers
	// are a bundle of race conditions and there's no need
	// to make things worse.
	if len(names) == 0 {
		c := &CPIO9PFID{path: last.path, fs: l.fs}
		qid, fi, err := c.info()
		verbose("Walk to %v: %v, %v, %v", *c, qid, fi, err)
		if err != nil {
			return nil, nil, err
		}
		qids = append(qids, qid)
		verbose("Walk: return %v, %v, nil", qids, last)
		return qids, last, nil
	}
	verbose("Walk: %v", names)
	fullpath := r.Info.Name
	verbose("Walk from %q: %q", fullpath, names)
	for _, name := range names {
		c := &CPIO9PFID{path: last.path, fs: l.fs}
		qid, fi, err := c.info()
		if err != nil {
			return nil, nil, err
		}
		fullpath = filepath.Join(fullpath, name)
		ix, ok := l.fs.m[fullpath]
		verbose("Walk to %q from %v: %v, %v, %v", fullpath, r, qid, fi, ok)
		if !ok {
			return nil, nil, os.ErrNotExist
		}
		qids = append(qids, qid)
		last.path = ix
	}
	verbose("Walk: return %v, %v, nil", qids, last)
	return qids, last, nil
}

// FSync implements p9.File.FSync.
func (l *CPIO9PFID) FSync() error {
	return nil
}

// Close implements p9.File.Close.
func (l *CPIO9PFID) Close() error {
	return nil
}

// Open implements p9.File.Open.
func (l *CPIO9PFID) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	qid, fi, err := l.info()
	verbose("Open %v: (%v, %v, %v", *l, qid, fi, err)
	if err != nil {
		return qid, 0, err
	}

	if mode != p9.ReadOnly {
		return qid, 0, os.ErrPermission
	}

	// Do the actual open.
	// from DIOD
	// if iounit=0, v9fs will use msize-P9_IOHDRSZ
	verbose("Open returns %v, 0, nil", qid)
	return qid, 0, nil
}

// Read implements p9.File.ReadAt.
func (l *CPIO9PFID) ReadAt(p []byte, offset int64) (int, error) {
	r, err := l.rec()
	if err != nil {
		return -1, err
	}
	return r.ReadAt(p, offset)
}

// Write implements p9.File.WriteAt.
func (l *CPIO9PFID) WriteAt(p []byte, offset int64) (int, error) {
	return -1, os.ErrPermission
}

// Create implements p9.File.Create.
func (l *CPIO9PFID) Create(name string, mode p9.OpenFlags, permissions p9.FileMode, _ p9.UID, _ p9.GID) (p9.File, p9.QID, uint32, error) {
	return nil, p9.QID{}, 0, os.ErrPermission
}

// Mkdir implements p9.File.Mkdir.
//
// Not properly implemented.
func (l *CPIO9PFID) Mkdir(name string, permissions p9.FileMode, _ p9.UID, _ p9.GID) (p9.QID, error) {
	return p9.QID{}, os.ErrPermission
}

// Symlink implements p9.File.Symlink.
//
// Not properly implemented.
func (l *CPIO9PFID) Symlink(oldname string, newname string, _ p9.UID, _ p9.GID) (p9.QID, error) {
	return p9.QID{}, os.ErrPermission
}

// Link implements p9.File.Link.
//
// Not properly implemented.
func (l *CPIO9PFID) Link(target p9.File, newname string) error {
	return os.ErrPermission
}

func (l *CPIO9PFID) readdir() ([]uint64, error) {
	verbose("readdir at %d", l.path)
	r, err := l.rec()
	if err != nil {
		return nil, err
	}
	dn := r.Info.Name
	verbose("readdir starts from %v %v", l, r)
	// while the name is a prefix of the records we are scanning,
	// append the record.
	// This can not be returned as a range as we do not want
	// contents of all subdirs.
	var list []uint64
	for i, r := range l.fs.recs[l.path+1:] {
		// filepath.Rel fails, we're done here.
		b, err := filepath.Rel(dn, r.Name)
		if err != nil {
			verbose("r.Name %q: DONE", r.Name)
			break
		}
		dir, _ := filepath.Split(b)
		if len(dir) > 0 {
			continue
		}
		verbose("readdir: %v", i)
		list = append(list, uint64(i)+l.path+1)
	}
	return list, nil
}

// Readdir implements p9.File.Readdir.
// This is a bit of a mess in cpio, but the good news is that
// files will be in some sort of order ...
func (l *CPIO9PFID) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	qid, _, err := l.info()
	if err != nil {
		return nil, err
	}
	list, err := l.readdir()
	if err != nil {
		return nil, err
	}
	verbose("readdir list %v", list)
	var dirents p9.Dirents
	dirents = append(dirents, p9.Dirent{
		QID:    qid,
		Type:   qid.Type,
		Name:   ".",
		Offset: l.path,
	})
	verbose("add path %d '.'", l.path)
	//log.Printf("readdir %q returns %d entries start at offset %d", l.path, len(fi), offset)
	for _, i := range list {
		entry := CPIO9PFID{path: i, fs: l.fs}
		qid, _, err := entry.info()
		if err != nil {
			continue
		}
		r, err := entry.rec()
		if err != nil {
			continue
		}
		verbose("add path %d %q", i, r.Info.Name)
		dirents = append(dirents, p9.Dirent{
			QID:    qid,
			Type:   qid.Type,
			Name:   r.Info.Name,
			Offset: i,
		})
	}

	return dirents, nil
}

// Readlink implements p9.File.Readlink.
func (l *CPIO9PFID) Readlink() (string, error) {
	return "", os.ErrPermission
}

// Flush implements p9.File.Flush.
func (l *CPIO9PFID) Flush() error {
	return nil
}

// Renamed implements p9.File.Renamed.
func (l *CPIO9PFID) Renamed(parent p9.File, newName string) {
}

// UnlinkAt implements p9.File.UnlinkAt.
func (l *CPIO9PFID) UnlinkAt(name string, flags uint32) error {
	return os.ErrPermission
}

// Mknod implements p9.File.Mknod.
func (*CPIO9PFID) Mknod(name string, mode p9.FileMode, major uint32, minor uint32, _ p9.UID, _ p9.GID) (p9.QID, error) {
	return p9.QID{}, syscall.ENOSYS
}

// Rename implements p9.File.Rename.
func (*CPIO9PFID) Rename(directory p9.File, name string) error {
	return syscall.ENOSYS
}

// RenameAt implements p9.File.RenameAt.
// There is no guarantee that there is not a zipslip issue.
func (l *CPIO9PFID) RenameAt(oldName string, newDir p9.File, newName string) error {
	return syscall.ENOSYS
}

// StatFS implements p9.File.StatFS.
//
// Not implemented.
func (*CPIO9PFID) StatFS() (p9.FSStat, error) {
	return p9.FSStat{}, syscall.ENOSYS
}

func (l *CPIO9PFID) SetAttr(mask p9.SetAttrMask, attr p9.SetAttr) error {
	return os.ErrPermission
}

// GetAttr implements p9.File.GetAttr.
//
// Not fully implemented.
func (l *CPIO9PFID) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	qid, fi, err := l.info()
	if err != nil {
		return qid, p9.AttrMask{}, p9.Attr{}, err
	}

	attr := p9.Attr{
		Mode:             p9.FileMode(fi.Mode),
		UID:              p9.UID(fi.UID),
		GID:              p9.GID(fi.GID),
		NLink:            p9.NLink(fi.NLink),
		RDev:             p9.Dev(fi.Dev),
		Size:             uint64(fi.FileSize),
		BlockSize:        uint64(4096),
		Blocks:           uint64(fi.FileSize / 4096),
		ATimeSeconds:     uint64(0),
		ATimeNanoSeconds: uint64(0),
		MTimeSeconds:     uint64(fi.MTime),
		MTimeNanoSeconds: uint64(0),
		CTimeSeconds:     0,
		CTimeNanoSeconds: 0,
	}
	valid := p9.AttrMask{
		Mode:   true,
		UID:    true,
		GID:    true,
		NLink:  true,
		RDev:   true,
		Size:   true,
		Blocks: true,
		ATime:  true,
		MTime:  true,
		CTime:  true,
	}

	return qid, valid, attr, nil
}
