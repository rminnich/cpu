// Copyright 2023 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/hugelgupf/p9/p9"
)

func TestCPIO9P(t *testing.T) {
	d := t.TempDir()
	bogus := filepath.Join(d, "bogus")
	if _, err := NewCPIO9P(bogus); err == nil {
		t.Errorf("Opening non-existent file: got nil, want err")
	}
	if err := ioutil.WriteFile(bogus, []byte("bogus"), 0666); err != nil {
		t.Fatal(err)
	}

	v = t.Logf
	if _, err := NewCPIO9P(bogus); err == nil {
		t.Errorf("Opening bad file: got nil, want err")
	}

	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	attach, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v", attach)

	_, root, err := attach.Walk([]string{})
	if err != nil {
		t.Fatalf("walking '': want nil, got %v", err)
	}

	if q, f, err := root.Walk([]string{"barf"}); err == nil {
		t.Fatalf("walking 'barf': want err, got (%v,%v,%v)", q, f, err)
	}

	_, b, err := root.Walk([]string{"b"})
	if err != nil {
		t.Fatalf("walking 'b': want nil, got %v", err)
	}
	t.Logf("b %v", b)

	q, c, err := root.Walk([]string{"b", "c"})
	if err != nil {
		t.Fatalf("walking a/b: want nil, got %v", err)
	}
	if len(q) != 2 {
		t.Fatalf("walking a/b: want 2 qids, got (%v,%v)", q, err)
	}
	if c == nil {
		t.Fatalf("walking a/b: want non-nil file, got nil")
	}

	var (
		of p9.OpenFlags
		m p9.FileMode
	)
	if _, _, _, err := root.Create("", of, m, p9.UID(0), p9.GID(0)); err == nil {
		t.Errorf("create in root: got hil, want err")
	}

	if _, err := root.Mkdir("", m, p9.UID(0), p9.GID(0)); err == nil {
		t.Errorf("mkdir in root: got hil, want err")
	}

	if _, _, err := c.Walk([]string{"d"}); err != nil {
		t.Fatalf("walking d from b/c: want nil, got %v", err)
	}

	_, hi, err := c.Walk([]string{"hi"})
	if err != nil {
		t.Fatalf("walking hi from b/c: want nil, got %v", err)
	}
	var data [2]byte
	off := int64(1)
	if _, err := hi.ReadAt(data[:], off); err != nil {
		t.Fatalf("Reading hi: want nil, got %v", err)
	}
	if n, _ := hi.ReadAt(data[:], off); n != 2 {
		t.Fatalf("Reading hi: want 2 bytes, got %v", n)
	}
	if string(data[:]) != "i\n" {
		t.Fatalf("Reading hi: want %q, got %q", "i\n", string(data[:]))
	}

	// test bad operators
	if n, err := hi.WriteAt(data[:], 0); err == nil || n != -1{
		t.Errorf("WriteAt: got (%d, nil), want (-1, err)", n)
	}

	if _, err := hi.Symlink("", "", p9.UID(0), p9.GID(0)); err == nil {
		t.Errorf("symlink: got nil, want err")
	}

	if  err := hi.Link(root, "", ); err == nil {
		t.Errorf("link: got nil, want err")
	}

	if  _, err := hi.Readlink(); err == nil {
		t.Errorf("remove: got nil, want err")
	}

	if  _, err := hi.Mknod("", m, 0, 0, p9.UID(0), p9.GID(0)); err == nil {
		t.Errorf("Mknod: got nil, want err")
	}

	if  err := hi.Rename(root, "k"); err == nil {
		t.Errorf("rename: got nil, want err")
	}

	if  err := hi.RenameAt("", root, ""); err == nil {
		t.Errorf("renameat: got nil, want err")
	}

	if  err := hi.UnlinkAt("hi", 0); err == nil {
		t.Errorf("unlinkat: got nil, want err")
	}

	if  _, err := hi.StatFS(); err == nil {
		t.Errorf("statfs: got nil, want err")
	}

	var (
		mask p9.SetAttrMask
		attr p9.SetAttr
	)

	if  err := hi.SetAttr(mask, attr); err == nil {
		t.Errorf("setattr: got nil, want err")
	}

	var am p9.AttrMask
	if _, _, _, err = hi.GetAttr(am); err != nil {
		t.Errorf("getattr: want nil, got %v", err)
	}

	dirs, err := c.Readdir(0, 64*1024)
	if err != nil {
		t.Fatalf("readdir on root: want nil, got %v", err)
	}
	if len(dirs) != 4 {
		t.Fatalf("readdir on root: want %d entries, got %d", 4, len(dirs))
	}
	t.Logf("readdir c/ %v", dirs)

	dirs, err = root.Readdir(0, 64*1024)
	if err != nil {
		t.Fatalf("readdir on root: want nil, got %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("readdir on root: want %d entries, got %d", 2, len(dirs))
	}
	t.Logf("readdir / %v", dirs)
}
