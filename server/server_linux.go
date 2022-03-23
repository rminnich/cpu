// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/sys/unix"
)

// cpud can run in one of three modes
// o init
// o daemon started by init
// o manager of one cpu session.
// It is *critical* that the session manager have a private
// name space, else every cpu session will interfere with every
// other session's mounts. What's the best way to ensure the manager
// gets a private name space, and ensure that no improper use
// of this package will result in NOT having a private name space?
// How do we make the logic failsafe?
//
// It turns out there is no harm in always privatizing the name space,
// no matter the mode.
// So in this init function, we do not parse flags (that breaks tests;
// flag.Parse() in init is a no-no), and then, no
// matter what, privatize the namespace, and mount a private /tmp/cpu if we
// are not pid1. As for pid1 tasks, they should be specified by the cpud
// itself, not this package. This code merely ensures correction operation
// of cpud no matter what mode it is invoked in.
func init() {
	// placeholder. It's not clear we ever want to do this. We used to create
	// a root file system here, but that should be up to the server. The files
	// might magically exist, b/c of initrd; or be automagically mounted via
	// some other mechanism.
	if os.Getpid() == 1 {
	}
}

// NameSpace assembles a NameSpace for this cpud, iff CPU_NONCE
// is set and len(s.binds) > 0.
// NOTE: this assumes we were started with CloneFlags set to CLONE_NEWNS.
// If you don't do that, you will be sad.
func (s *Session) Namespace() (error, error) {
	if len(s.binds) == 0 {
		return nil, nil
	}
	// Get the nonce and remove it from the environment.
	// N.B. We do not save the nonce in the cpu struct.
	nonce, ok := os.LookupEnv("CPUNONCE")
	if !ok {
		return nil, nil
	}
	os.Unsetenv("CPUNONCE")
	v("CPUD:namespace is %q", s.binds)

	// Connect to the socket, return the nonce.
	a := net.JoinHostPort("127.0.0.1", s.port9p)
	v("CPUD:Dial %v", a)
	so, err := net.Dial("tcp4", a)
	if err != nil {
		return nil, fmt.Errorf("CPUD:Dial 9p port: %v", err)
	}
	v("CPUD:Connected: write nonce %s\n", nonce)
	if _, err := fmt.Fprintf(so, "%s", nonce); err != nil {
		return nil, fmt.Errorf("CPUD:Write nonce: %v", err)
	}
	v("CPUD:Wrote the nonce")
	// Zero it. I realize I am not a crypto person.
	// improvements welcome.
	copy([]byte(nonce), make([]byte, len(nonce)))

	// the kernel takes over the socket after the Mount.
	defer so.Close()
	flags := uintptr(unix.MS_NODEV | unix.MS_NOSUID)
	cf, err := so.(*net.TCPConn).File()
	if err != nil {
		return nil, fmt.Errorf("CPUD:Cannot get fd for %v: %v", so, err)
	}

	fd := cf.Fd()
	v("CPUD:fd is %v", fd)

	user := os.Getenv("USER")
	if user == "" {
		user = "nouser"
	}

	// The debug= option is here so you can see how to temporarily set it if needed.
	// It generates copious output so use it sparingly.
	// A useful compromise value is 5.
	opts := fmt.Sprintf("version=9p2000.L,trans=fd,rfdno=%d,wfdno=%d,uname=%v,debug=0,msize=%d", fd, fd, user, s.msize)
	if len(s.mopts) > 0 {
		opts += "," + s.mopts
	}
	v("CPUD: mount 127.0.0.1 on /tmp/cpu 9p %#x %s", flags, opts)
	if err := unix.Mount("127.0.0.1", "/tmp/cpu", "9p", flags, opts); err != nil {
		return nil, fmt.Errorf("9p mount %v", err)
	}
	v("CPUD: mount done")

	// In some cases if you set LD_LIBRARY_PATH it is ignored.
	// This is disappointing to say the least. We just bind a few things into /
	// bind *may* hide local resources but for now it's the least worst option.
	var warning error
	for _, n := range s.binds {
		t := filepath.Join("/tmp/cpu", n.Remote)
		v("CPUD: mount %v over %v", t, n.Local)
		if err := unix.Mount(t, n.Local, "", syscall.MS_BIND, ""); err != nil {
			s.fail = true
			warning = multierror.Append(fmt.Errorf("CPUD:Warning: mounting %v on %v failed: %v", t, n, err))
		} else {
			v("CPUD:Mounted %v on %v", t, n)
		}

	}
	return warning, nil
}

func osMounts() error {
	var errors error
	if err := unix.Mount("cpu", "/tmp", "tmpfs", 0, ""); err != nil {
		errors = fmt.Errorf("CPUD:Warning: tmpfs mount on /tmp (%v) failed. There will be no 9p mount", err)

	}

	// Further, bind / onto /tmp/local so a non-hacked-on version may be visible.
	if err := unix.Mount("/", "/tmp/local", "", syscall.MS_BIND, ""); err != nil {
		errors = multierror.Append(fmt.Errorf("CPUD:Warning: binding / over /tmp/cpu did not work: %v, continuing anyway", err))
	}
	return errors
}

// func logopts() {
// 	if *klog {
// 		ulog.KernelLog.Reinit()
// 		v = ulog.KernelLog.Printf
// 	}
// }

func command(n string, args ...string) *exec.Cmd {
	cmd := exec.Command(n, args...)
	// N.B.: in the go runtime, after not long ago, CLONE_NEWNS in the CloneFlags
	// also does two things: an unshare, and a remount of / to unshare mounts.
	// see d8ed449d8eae5b39ffe227ef7f56785e978dd5e2 in the go tree for a discussion.
	// This meant we could remove ALL calls of unshare and mount from cpud.
	// Fun fact: I wrote that fix years ago, and then forgot to remove
	// the support code from cpu. Oops.
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS}
	return cmd
}
