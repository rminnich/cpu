// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/hugelgupf/p9/p9"
	"github.com/jacobsa/fuse"
	"golang.org/x/sys/unix"
)

var FUSE bool

// Namespace assembles a NameSpace for this cpud, iff CPU_NONCE
// is set and len(s.binds) > 0.
//
// This code assumes you have a non-shared namespace. This is
// archieved in go by setting exec.Cmd.SysprocAttr.Unshareflags to
// CLONE_NEWNS; the go runtime will then do what is needed to
// privatize a namespace. I can say this because I wrote that code 5
// years ago, and go tests for it are run as part of the go
// release process.
//
// To reiterate, this package requires, for proper operation, that the
// process using it be in a private name space, and, further, that the
// namespace can't magically be reshared.
//
// It's very hard and probably impossible to test for a namespace
// being set up properly on Linux. On plan 9 it's easy: read the
// process namespace file and see if it's empty. But no such operation
// is possible on Linux and, worse, since sometime in the 3.x kernel
// series, even once a namespace is unshared, another process can
// start using it via nsenter(2).
//
// Hence this note: it is a warning to our future selves or users of
// this package.
//
// Note, however, that cpud does the right thing, by setting
// Unshareflags to CLONE_NEWNS. Tests in the cpu server code ensure
// that continues to be the case.
//
// tl;dr: Linux namespaces are a pretty terrible mess. They may have
// been inspired by Plan 9, but an understanding of some critical core
// ideas has been lost. As a result, they do not remotely represent
// any kind of security boundary.
func (s *Session) Namespace() (error, error) {
	// Get the nonce and remove it from the environment.
	// N.B. We do not save the nonce in the cpu struct.
	nonce, ok := os.LookupEnv("CPUNONCE")
	if !ok {
		return nil, nil
	}
	os.Unsetenv("CPUNONCE")
	verbose("namespace is %q", s.binds)

	// Connect to the socket, return the nonce.
	a := net.JoinHostPort("localhost", s.port9p)
	verbose("Dial %v", a)
	so, err := net.Dial("tcp", a)
	if err != nil {
		return nil, fmt.Errorf("CPUD:Dial 9p port: %v", err)
	}
	verbose("Connected: write nonce %s\n", nonce)
	if _, err := fmt.Fprintf(so, "%s", nonce); err != nil {
		return nil, fmt.Errorf("CPUD:Write nonce: %v", err)
	}
	verbose("Wrote the nonce")
	// Zero it. I realize I am not a crypto person.
	// improvements welcome.
	copy([]byte(nonce), make([]byte, len(nonce)))

	cf, err := so.(*net.TCPConn).File()
	if err != nil {
		return nil, fmt.Errorf("CPUD:Cannot get fd for %v: %v", so, err)
	}

	fd := cf.Fd()
	verbose("fd is %v", fd)

	user := os.Getenv("USER")
	if user == "" {
		user = "nouser"
	}

	// test value for trying out FUSE to 9p
	// This has an advantage that FUSE has good integration with
	// the kernel page cache, and, further, we can implement
	// readahead in cpud.
	mountTarget := filepath.Join(s.tmpMnt, "cpu")
	if os.Getenv("CPUD_FUSE") != "" {
		v = log.Printf
		FUSE = true
	}
	if FUSE {
		verbose("CPUD: using FUSE to 9P gateway")
		// When we get here, the FD has been verified.
		// The 9p version and attach need to run.
		cl, err := p9.NewClient(cf, p9.WithMessageSize(128*1024))
		if err != nil {
			return nil, err
		}
		root, err := cl.Attach("/")
		if err != nil {
			return nil, err
		}

		s.cl = cl
		s.root = root

		fs, cfs, err := NewP9FS(cl, root, 5*time.Second, 5*time.Second)
		if err != nil {
			return nil, err
		}

		s.fs = fs
		s.cfs = cfs
		// This will need to move to the kernel-independent part at some point.
		c := &fuse.MountConfig{
			ErrorLogger: log.Default(),
			DebugLogger: log.Default(),
			// This must be set, else you will get
			// fuse: Bad value for 'source'
			// and the mount will fail
			FSName: "cpud",
		}
		mfs, err := fuse.Mount(mountTarget, fs, c)
		if err != nil {
			return nil, err
		}
		s.mfs = mfs
		// annoying but clean up later.
		s.cfs.inMap[1] = entry{
			fid:  root,
			root: true,
			ino:  1,
		}
	} else {
		verbose("CPUD: using 9P")
		// the kernel takes over the socket after the Mount.
		defer so.Close()

		flags := uintptr(unix.MS_NODEV | unix.MS_NOSUID)
		fd := cf.Fd()
		verbose("CPUD:fd is %v", fd)

		// The debug= option is here so you can see how to temporarily set it if needed.
		// It generates copious output so use it sparingly.
		// A useful compromise value is 5.
		opts := fmt.Sprintf("version=9p2000.L,trans=fd,rfdno=%d,wfdno=%d,uname=%v,debug=0,msize=%d", fd, fd, user, s.msize)
		if len(s.mopts) > 0 {
			opts += "," + s.mopts
		}
		verbose("CPUD: mount 127.0.0.1 on %s 9p %#x %s", mountTarget, flags, opts)
		if err := unix.Mount("localhost", mountTarget, "9p", flags, opts); err != nil {
			return nil, fmt.Errorf("9p mount %v", err)
		}
	}
	verbose("mount done")

	// In some cases if you set LD_LIBRARY_PATH it is ignored.
	// This is disappointing to say the least. We just bind a few things into /
	// bind *may* hide local resources but for now it's the least worst option.
	var warning error
	for _, n := range s.binds {
		t := filepath.Join(mountTarget, n.Remote)
		verbose("mount %v over %v", t, n.Local)
		if err := unix.Mount(t, n.Local, "", syscall.MS_BIND, ""); err != nil {
			s.fail = true
			warning = multierror.Append(fmt.Errorf("CPUD:Warning: mounting %v on %v failed: %v", t, n, err))
		} else {
			verbose("Mounted %v on %v", t, n)
		}

	}
	return warning, nil
}

func osMounts(tmpMnt string) error {
	var errors error
	// Further, bind / onto /tmp/local so a non-hacked-on version may be visible.
	if err := unix.Mount("/", filepath.Join(tmpMnt, "local"), "", syscall.MS_BIND, ""); err != nil {
		errors = multierror.Append(fmt.Errorf("CPUD:Warning: binding / over %s did not work: %v, continuing anyway", filepath.Join(tmpMnt, "local"), err))
	}
	return errors
}

// runSetup performs kernel-specific operations for starting a Session.
func runSetup(tmpMnt string) error {
	if err := os.MkdirAll(tmpMnt, 0666); err != nil && !os.IsExist(err) {
		return fmt.Errorf("cannot create %s: %v", tmpMnt, err)
	}
	if err := unix.Mount("cpu", tmpMnt, "tmpfs", 0, ""); err != nil {
		return fmt.Errorf(`unix.Mount("cpu", %s, "tmpfs", 0, ""); %v != nil`, tmpMnt, err)
	}
	return nil
}
