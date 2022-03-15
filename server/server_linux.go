// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/hashicorp/go-multierror"
	"github.com/u-root/u-root/pkg/ulog"
	"golang.org/x/sys/unix"
)

var (
	klog = flag.Bool("klog", false, "Log cpud messages in kernel log, not stdout")
)

// cpud can run in one of three modes
// o init
// o daemon started by init
// o manager of one cpu session.
// It is *critical* that the session manager have a private
// name space, else every cpu session will interfere with every
// other session's mounts. What's the best way to ensure the manager
// gets a private name space?
// It turns out there is no harm in always privatizing the name space,
// no matter the mode. The only special case is for init, as init
// has to set up global mounts. It is easy to test for init: see
// if we are PID 1.
// So in this init function, we do not parse flags (that breaks tests;
// flag.Parse() in init is a no-no), we will do PID1 tasks and then, no
// matter what, privatize the namespace.
func init() {
	if os.Getpid() == 1 {
	}
	privatize()
	if os.Getpid() != 1 {

		if err := unix.Mount("cpu", "/tmp", "tmpfs", 0, ""); err != nil {
			log.Fatalf(`unix.Mount("cpu", "/tmp", "tmpfs", 0, ""); %v != nil`, err)
		}
	}

}

// NameSpace assembles a NameSpace for this cpud, iff CPU_NAMESPACE
// is set.
// CPU_NAMESPACE can be the empty string.
// It also requires that CPU_NONCE exist.
func (s *Server) Namespace(bindover string) (error, error) {
	var warning error
	// Get the nonce and remove it from the environment.
	// N.B. We do not save the nonce in the cpu struct.
	nonce := os.Getenv("CPUNONCE")
	os.Unsetenv("CPUNONCE")
	v("CPUD:namespace is %q", bindover)

	// Connect to the socket, return the nonce.
	a := net.JoinHostPort("127.0.0.1", *port9p)
	v("CPUD:Dial %v", a)
	so, err := net.Dial("tcp4", a)
	if err != nil {
		return warning, fmt.Errorf("CPUD:Dial 9p port: %v", err)
	}
	v("CPUD:Connected: write nonce %s\n", nonce)
	if _, err := fmt.Fprintf(so, "%s", nonce); err != nil {
		return warning, fmt.Errorf("CPUD:Write nonce: %v", err)
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
		return warning, fmt.Errorf("CPUD:Cannot get fd for %v: %v", so, err)
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
	opts := fmt.Sprintf("version=9p2000.L,trans=fd,rfdno=%d,wfdno=%d,uname=%v,debug=0,msize=%d", fd, fd, user, *msize)
	if *mountopts != "" {
		opts += "," + *mountopts
	}
	v("CPUD: mount 127.0.0.1 on /tmp/cpu 9p %#x %s", flags, opts)
	if err := unix.Mount("127.0.0.1", "/tmp/cpu", "9p", flags, opts); err != nil {
		return warning, fmt.Errorf("9p mount %v", err)
	}
	v("CPUD: mount done")

	// In some cases if you set LD_LIBRARY_PATH it is ignored.
	// This is disappointing to say the least. We just bind a few things into /
	// bind *may* hide local resources but for now it's the least worst option.
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

func logopts() {
	if *klog {
		ulog.KernelLog.Reinit()
		v = ulog.KernelLog.Printf
	}
}

func privatize() {
	// The unshare system call in Linux doesn't unshare mount points
	// mounted with --shared. Systemd mounts / with --shared. For a
	// long discussion of the pros and cons of this see debian bug 739593.
	// The Go model of unsharing is more like Plan 9, where you ask
	// to unshare and the namespaces are unconditionally unshared.
	// To make this model work we must further mark / as MS_PRIVATE.
	// This is what the standard unshare command does.
	var (
		none  = [...]byte{'n', 'o', 'n', 'e', 0}
		slash = [...]byte{'/', 0}
		flags = uintptr(unix.MS_PRIVATE | unix.MS_REC) // Thanks for nothing Linux.
	)
	// We assume that this was called via an unshare command or forked by
	// a process with the CLONE_NEWS flag set. This call to Unshare used to work;
	// no longer. We leave this code here as a signpost. Don't enable it.
	// It won't work. Go's green threads and Linux name space code have
	// never gotten along. Fixing it is hard, I've discussed this with the Go
	// core from time to time and it's not a priority for them.
	if err := syscall.Unshare(syscall.CLONE_NEWNS); err != nil {
		log.Printf("CPUD:bad Unshare: %v", err)
	}
	// Make / private. This call *is* safe so far for reasons.
	// Probably because, on many systems, we are lucky enough not to have a systemd
	// there screwing up namespaces.
	_, _, err1 := syscall.RawSyscall6(unix.SYS_MOUNT, uintptr(unsafe.Pointer(&none[0])), uintptr(unsafe.Pointer(&slash[0])), 0, flags, 0, 0)
	if err1 != 0 {
		log.Printf("CPUD:Warning: unshare failed (%v). There will be no private 9p mount if systemd is there", err1)
	}
}

func command(n string, args ...string) *exec.Cmd {
	cmd := exec.Command(n, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS}
	return cmd
}
