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
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hugelgupf/p9/p9"
)

// CPU9P is a p9.Attacher.
type CPU9P struct {
	p9.DefaultWalkGetAttr

	path string
	file *os.File
}

// NewCPU9P returns a CPU9P, properly initialized.
func NewCPU9P(root string) *CPU9P {
	return &CPU9P{path: root}
}

// Attach implements p9.Attacher.Attach.
func (l *CPU9P) Attach() (p9.File, error) {
	return &CPU9P{path: l.path}, nil
}

var (
	_ p9.File     = &CPU9P{}
	_ p9.Attacher = &CPU9P{}
)

// info constructs a QID for this file.
func (l *CPU9P) info() (p9.QID, os.FileInfo, error) {
	var (
		qid p9.QID
		fi  os.FileInfo
		err error
	)

	// Stat the file.
	if l.file != nil {
		fi, err = l.file.Stat()
	} else {
		fi, err = os.Lstat(l.path)
	}
	if err != nil {
		//log.Printf("error stating %#v: %v", l, err)
		return qid, nil, err
	}

	// Construct the QID type.
	qid.Type = p9.ModeFromOS(fi.Mode()).QIDType()

	// Save the path from the Ino.
	qid.Path = fi.Sys().(*syscall.Stat_t).Ino
	return qid, fi, nil
}

// Walk implements p9.File.Walk.
func (l *CPU9P) Walk(names []string) ([]p9.QID, p9.File, error) {
	var qids []p9.QID
	last := &CPU9P{path: l.path}
	// If the names are empty we return info for l
	// An extra stat is never hurtful; all servers
	// are a bundle of race conditions and there's no need
	// to make things worse.
	if len(names) == 0 {
		c := &CPU9P{path: last.path}
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
	for _, name := range names {
		c := &CPU9P{path: filepath.Join(last.path, name)}
		qid, fi, err := c.info()
		verbose("Walk to %v: %v, %v, %v", *c, qid, fi, err)
		if err != nil {
			return nil, nil, err
		}
		qids = append(qids, qid)
		last = c
	}
	verbose("Walk: return %v, %v, nil", qids, last)
	return qids, last, nil
}

// FSync implements p9.File.FSync.
func (l *CPU9P) FSync() error {
	return l.file.Sync()
}

// Close implements p9.File.Close.
func (l *CPU9P) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Open implements p9.File.Open.
func (l *CPU9P) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	qid, fi, err := l.info()
	verbose("Open %v: (%v, %v, %v", *l, qid, fi, err)
	if err != nil {
		return qid, 0, err
	}

	flags := osflags(fi, mode)
	// Do the actual open.
	f, err := os.OpenFile(l.path, flags, 0)
	verbose("Open(%v, %v, %v): (%v, %v", l.path, flags, 0, f, err)
	if err != nil {
		return qid, 0, err
	}
	l.file = f
	// from DIOD
	// if iounit=0, v9fs will use msize-P9_IOHDRSZ
	verbose("Open returns %v, 0, nil", qid)
	return qid, 0, nil
}

// Read implements p9.File.ReadAt.
func (l *CPU9P) ReadAt(p []byte, offset int64) (int, error) {
	return l.file.ReadAt(p, int64(offset))
}

// Write implements p9.File.WriteAt.
// There is a very rare case where O_APPEND files are written more than
// once, and we get an error. That error is generated by the Go runtime,
// after checking the open flag in the os.File struct.
// I.e. the error is not generated by a system call,
// so it is very cheap to try the WriteAt, check the
// error, and call Write if it is the rare case of a second write
// to an append-only file..
func (l *CPU9P) WriteAt(p []byte, offset int64) (int, error) {
	n, err := l.file.WriteAt(p, int64(offset))
	if err != nil {
		if strings.Contains(err.Error(), "os: invalid use of WriteAt on file opened with O_APPEND") {
			return l.file.Write(p)
		}
	}
	return n, err
}

// Create implements p9.File.Create.
func (l *CPU9P) Create(name string, mode p9.OpenFlags, permissions p9.FileMode, _ p9.UID, _ p9.GID) (p9.File, p9.QID, uint32, error) {
	f, err := os.OpenFile(filepath.Join(l.path, name), os.O_CREATE|mode.OSFlags(), os.FileMode(permissions))
	if err != nil {
		return nil, p9.QID{}, 0, err
	}

	l2 := &CPU9P{path: filepath.Join(l.path, name), file: f}
	qid, _, err := l2.info()
	if err != nil {
		l2.Close()
		return nil, p9.QID{}, 0, err
	}

	// from DIOD
	// if iounit=0, v9fs will use msize-P9_IOHDRSZ
	return l2, qid, 0, nil
}

// Mkdir implements p9.File.Mkdir.
//
// Not properly implemented.
func (l *CPU9P) Mkdir(name string, permissions p9.FileMode, _ p9.UID, _ p9.GID) (p9.QID, error) {
	if err := os.Mkdir(filepath.Join(l.path, name), os.FileMode(permissions)); err != nil {
		return p9.QID{}, err
	}

	// Blank QID.
	return p9.QID{}, nil
}

// Symlink implements p9.File.Symlink.
//
// Not properly implemented.
func (l *CPU9P) Symlink(oldname string, newname string, _ p9.UID, _ p9.GID) (p9.QID, error) {
	if err := os.Symlink(oldname, filepath.Join(l.path, newname)); err != nil {
		return p9.QID{}, err
	}

	// Blank QID.
	return p9.QID{}, nil
}

// Link implements p9.File.Link.
//
// Not properly implemented.
func (l *CPU9P) Link(target p9.File, newname string) error {
	return os.Link(target.(*CPU9P).path, filepath.Join(l.path, newname))
}

// Readdir implements p9.File.Readdir.
func (l *CPU9P) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	fi, err := ioutil.ReadDir(l.path)
	if err != nil {
		return nil, err
	}
	var dirents p9.Dirents
	//log.Printf("readdir %q returns %d entries start at offset %d", l.path, len(fi), offset)
	for i := int(offset); i < len(fi); i++ {
		entry := CPU9P{path: filepath.Join(l.path, fi[i].Name())}
		qid, _, err := entry.info()
		if err != nil {
			continue
		}
		dirents = append(dirents, p9.Dirent{
			QID:    qid,
			Type:   qid.Type,
			Name:   fi[i].Name(),
			Offset: uint64(i + 1),
		})
	}

	return dirents, nil
}

// Readlink implements p9.File.Readlink.
func (l *CPU9P) Readlink() (string, error) {
	n, err := os.Readlink(l.path)
	if false && err != nil {
		log.Printf("Readlink(%v): %v, %v", *l, n, err)
	}
	return n, err
}

// Flush implements p9.File.Flush.
func (l *CPU9P) Flush() error {
	return nil
}

// Renamed implements p9.File.Renamed.
func (l *CPU9P) Renamed(parent p9.File, newName string) {
	l.path = filepath.Join(parent.(*CPU9P).path, newName)
}

// Remove implements p9.File.Remove
func (l *CPU9P) Remove() error {
	err := os.Remove(l.path)
	verbose("Remove(%q): (%v)", l.path, err)
	return err
}

// UnlinkAt implements p9.File.UnlinkAt.
// The flags docs are not very clear, but we
// always block on the unlink anyway.
func (l *CPU9P) UnlinkAt(name string, flags uint32) error {
	f := filepath.Join(l.path, name)
	err := os.Remove(f)
	verbose("UnlinkAt(%q=(%q, %q), %#x): (%v)", f, l.path, name, flags, err)
	return err
}

// Mknod implements p9.File.Mknod.
func (*CPU9P) Mknod(name string, mode p9.FileMode, major uint32, minor uint32, _ p9.UID, _ p9.GID) (p9.QID, error) {
	verbose("Mknod: not implemented")
	return p9.QID{}, syscall.ENOSYS
}

// Rename implements p9.File.Rename.
func (*CPU9P) Rename(directory p9.File, name string) error {
	verbose("Rename: not implemented")
	return syscall.ENOSYS
}

// RenameAt implements p9.File.RenameAt.
// There is no guarantee that there is not a zipslip issue.
func (l *CPU9P) RenameAt(oldName string, newDir p9.File, newName string) error {
	oldPath := path.Join(l.path, oldName)
	nd, ok := newDir.(*CPU9P)
	if !ok {
		// This is extremely serious and points to an internal error.
		// Hence the non-optional log.Printf. It should not ever happen.
		log.Printf("Can not happen: cast of newDir to %T failed; it is type %T", l, newDir)
		return os.ErrInvalid
	}
	newPath := path.Join(nd.path, newName)

	return os.Rename(oldPath, newPath)
}

// StatFS implements p9.File.StatFS.
//
// Not implemented.
func (*CPU9P) StatFS() (p9.FSStat, error) {
	verbose("StatFS: not implemented")
	return p9.FSStat{}, syscall.ENOSYS
}
