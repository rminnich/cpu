// Copyright 2023 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/u-root/cpu/mount"
	nfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"
)

func InitNFS(t testing.TB) (func() error, string) {
	t.Helper()
	// The osnfs will be absolute, so the mountdir has to be
	// relative to the osnfs
	dir := "data"
	mdir, err := filepath.Rel("/", dir)
	if err != nil {
		t.Fatal(err)
	}
	osfs := NewOSFS(dir)
	t.Logf("Create New OSFS @ %q with relative mount %q", dir, mdir)
	mem, err := NewfsCPIO("data/a.cpio", WithMount(mdir, osfs))
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		// If ipv4 isn't available, try ipv6.  It's not enough
		// to use Listen("tcp", "localhost:0a)", since we (the
		// cpu client) might have v4 (which the runtime will
		// use if we say "localhost"), but the server (cpud)
		// might not.
		l, err = net.Listen("tcp", "[::1]:0")
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("listener %v", l.Addr().String())
	ap := strings.Split(l.Addr().String(), ":")
	if len(ap) == 0 {
		t.Fatalf("SrvNFS:Can't find a port number in %v", l.Addr().String())
	}
	portnfs, err := strconv.ParseUint(ap[len(ap)-1], 0, 16)
	if err != nil {
		t.Fatalf("Can't find a 16-bit port number in %v", l.Addr().String())
	}
	t.Logf("listener %T %v addr %v port %v", l, l, l.Addr().String(), portnfs)
	u, err := uuid.NewRandom()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNullAuthHandler(l, COS{mem}, u.String())
	cacheHelper := nfshelper.NewCachingHandler(handler, 1024*1024)
	f := func() error {
		return nfs.Serve(l, cacheHelper)
	}
	fstab := fmt.Sprintf("127.0.0.1:%s %%s nfs rw,relatime,vers=3,rsize=1048576,wsize=1048576,namlen=255,hard,nolock,proto=tcp,port=%d,timeo=600,retrans=2,sec=sys,mountaddr=127.0.0.1,mountvers=3,mountport=%d,mountproto=tcp,local_lock=all,addr=127.0.0.1 0 0\n", u, portnfs, portnfs)
	return f, fstab
}

// Test with just one Mount
// don't bother with the zero case, NewUnionNFS does not allow it.
func TestNFSOne(t *testing.T) {
	d := t.TempDir()
	f, tab := InitNFS(t)
	fstab := fmt.Sprintf(tab, d)
	go f()
	if err := mount.Mount(fstab); err != nil {
		t.Fatalf("fstab mount failure: %v", err)
	}
}
