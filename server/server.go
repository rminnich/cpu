// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	// We use this ssh because it implements port redirection.
	// It can not, however, unpack password-protected keys yet.
	"github.com/gliderlabs/ssh"
	"github.com/hashicorp/go-multierror"
	"github.com/kr/pty" // TODO: get rid of krpty
	"github.com/u-root/u-root/pkg/termios"
	"golang.org/x/sys/unix"
)

const (
	defaultPort = "23"
)

// Bind defines a bind mount. It records the Local directory,
// e.g. /bin, and the remote directory, e.g. /tmp/cpu/bin.
type Bind struct {
	Local  string
	Remote string
}

// N.B. we used to have a Server type. But at some point I realized
// that a cpu server is just an SSH server with a special handler.
// Because one specializes the SSH server with cpu-specific attributes
// such as port and handler, further containing that SSH server in a
// CPU server led to awkwardness: the cpu Server struct contained
// an SSH server struct which contained cpu-specific entities.
// In the end, a CPU server is just an SSH server with some defaults
// changed, so for now, we just make the cpu Server type an SSH
// server type. Making our own type gives us an escape route
// in the future should we realize we made a mistake.

// Session is one instance of a cpu session, started by a cpud.
type Session struct {
	restorer *termios.Termios
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	binds    []Bind
	// Any function can use fail to mark that something
	// went badly wrong in some step. At that point, if wtf is set,
	// cpud will start it. This is incredibly handy for debugging.
	fail   bool
	msize  int
	mopts  string
	port9p string
	cmd    string
	args   []string
}

var (
	v = log.Printf // func(string, ...interface{}) {}
	// To get debugging when Things Go Wrong, you can run as, e.g., -wtf /bbin/elvish
	// or change the value here to /bbin/elvish.
	// This way, when Things Go Wrong, you'll be dropped into a shell and look around.
	// This is sometimes your only way to debug if there is (e.g.) a Go runtime
	// bug around unsharing. Which has happened.
	// This is compile time only because I'm so uncertain of whether it's dangerous
	wtf string
)

func verbose(f string, a ...interface{}) {
	v("\r\nCPUD:"+f+"\r\n", a...)
}

// DropPrivs drops privileges to the level of os.Getuid / os.Getgid
func (s *Session) DropPrivs() error {
	uid := unix.Getuid()
	v("CPUD:dropPrives: uid is %v", uid)
	if uid == 0 {
		v("CPUD:dropPrivs: not dropping privs")
		return nil
	}
	gid := unix.Getgid()
	v("CPUD:dropPrivs: gid is %v", gid)
	if err := unix.Setreuid(-1, uid); err != nil {
		return err
	}
	return unix.Setregid(-1, gid)
}

// Terminal sets up an interactive terminal.
func (s *Session) Terminal() error {
	// for some reason echo is not set.
	t, err := termios.New()
	if err != nil {
		return fmt.Errorf("CPUD:can't get a termios; oh well; %v", err)
	}
	term, err := t.Get()
	if err != nil {
		return fmt.Errorf("CPUD:can't get a termios; oh well; %v", err)
	}
	s.restorer = term
	term.Lflag |= unix.ECHO | unix.ECHONL
	if err := t.Set(term); err != nil {
		return fmt.Errorf("CPUD:can't set a termios; oh well; %v", err)
	}
	return nil
}

// TmpMounts sets up directories, and bind mounts, in /tmp/cpu.
// N.B. the /tmp/cpu mount is private assuming this program
// was started correctly with the namespace unshared (on Linux and
// Plan 9; on *BSD or Windows no such guarantees can be made).
func (s *Session) TmpMounts() error {
	// It's true we are making this directory while still root.
	// This ought to be safe as it is a private namespace mount.
	// (or we are started with a clean namespace).
	for _, n := range []string{"/tmp/cpu", "/tmp/local", "/tmp/merge", "/tmp/root", "/home"} {
		if err := os.MkdirAll(n, 0666); err != nil && !os.IsExist(err) {
			log.Println(err)
		}
	}

	if err := osMounts(); err != nil {
		log.Println(err)
	}
	return nil
}

// Run starts up a remote cpu session. It is started by a cpu
// daemon via a -remote switch.
// This code assumes that cpud is running as init, or that
// an init has started a cpud, and that the code is running
// with a private namespace (CLONE_NEWS on Linux; RFNAMEG on Plan9).
// On Linux, it starts as uid 0, and once the mount/bind is done,
// calls DropPrivs.
func (s *Session) Run() error {
	var errors error

	if err := runSetup(); err != nil {
		return err
	}
	// N.B. if the namespace variable is set,
	// even if it is empty, server will try to do
	// the 9p mount.
	if b, ok := os.LookupEnv("CPU_NAMESPACE"); ok {
		v("Set up a namespace")
		if err := s.TmpMounts(); err != nil {
			s.fail = true
			errors = multierror.Append(err)
		}

		binds, err := ParseBinds(b)
		if err != nil {
			v("ParseBind failed: %v", err)
			s.fail = true
			errors = multierror.Append(errors, err)
		}

		s.binds = binds
		w, err := s.Namespace()
		if err != nil {
			return fmt.Errorf("CPUD:Namespace: warnings %v, err %v", w, multierror.Append(errors, err))
		}
		v("CPUD:warning: %v", w)

	}
	v("CPUD: bind mounts done")
	if err := s.Terminal(); err != nil {
		s.fail = true
		errors = multierror.Append(err)
	}
	v("CPUD: Terminal ready")
	if s.fail && len(wtf) != 0 {
		c := exec.Command(wtf)
		// Tricky question: should wtf use the os files are the ones
		// in the Server ... hmm.
		c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, "/"
		log.Printf("CPUD: WTF: try to run %v", c)
		if err := c.Run(); err != nil {
			log.Printf("CPUD: Running %q failed: %v", wtf, err)
		}
		log.Printf("CPUD: WTF done")
		return errors
	}
	// We don't want to run as the wrong uid.
	if err := s.DropPrivs(); err != nil {
		return multierror.Append(errors, err)
	}

	// The unmount happens for free since we unshared.
	v("CPUD:runRemote: command is %q", s.args)
	c := exec.Command(s.cmd, s.args...)
	c.Stdin, c.Stdout, c.Stderr, c.Dir = s.Stdin, s.Stdout, s.Stderr, os.Getenv("PWD")
	err := c.Run()
	v("CPUD:Run %v returns %v", c, err)
	if err != nil {
		if s.fail && len(wtf) != 0 {
			c := exec.Command(wtf)
			c.Stdin, c.Stdout, c.Stderr, c.Dir = os.Stdin, os.Stdout, os.Stderr, "/"
			log.Printf("CPUD: WTF: try to run %v", c)
			if err := c.Run(); err != nil {
				log.Printf("CPUD: Running %q failed: %v", wtf, err)
			}
			log.Printf("CPUD: WTF done: %v", err)
		}
	}
	return err
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ), //nolint
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

// errval can be used to examine errors that we don't consider errors
func errval(err error) error {
	if err == nil {
		return err
	}
	// Our zombie reaper is occasionally sneaking in and grabbing the
	// child's exit state. Looks like our process code still sux.
	if strings.Contains(err.Error(), "no child process") {
		return nil
	}
	return err
}

func handler(s ssh.Session) {
	a := s.Command()
	v("handler: cmd is %v", a)
	cmd := command(a[0], a[1:]...)
	cmd.Env = append(cmd.Env, s.Environ()...)
	ptyReq, winCh, isPty := s.Pty()
	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		f, err := pty.Start(cmd)
		v("command started with pty")
		if err != nil {
			v("CPUD:err %v", err)
			return
		}
		go func() {
			for win := range winCh {
				setWinsize(f, win.Width, win.Height)
			}
		}()
		go func() {
			io.Copy(f, s) //nolint stdin
		}()
		io.Copy(s, f) //nolint stdout
		// Stdout is closed, "there's no more to the show/
		// If you all want to breath right/you all better go"
		// This is going to seem a bit odd, but it is important to
		// only wait for the process started here, not any orphans.
		// In most cases, that process is either a singleton (so the wait
		// will be all we need); a shell (which does all the waiting for
		// its children); or the rare case of a detached process (in which
		// case the reaper will get it).
		// Seen in the wild: were this code to wait for orphans,
		// and the main loop to wait for orphans, they end up
		// competing with each other and the results are odd to say the least.
		// If the command exits, leaving orphans behind, it is the job
		// of the reaper to get them.
		v("wait for %v", cmd)
		err = cmd.Wait()
		v("cmd %v returns with %v %v", err, cmd, cmd.ProcessState)
		if errval(err) != nil {
			v("CPUD:child exited with  %v", err)
			s.Exit(cmd.ProcessState.ExitCode()) //nolint
		}

	} else {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
		v("running command without pty")
		if err := cmd.Run(); errval(err) != nil {
			v("CPUD:err %v", err)
			s.Exit(1) //nolint
		}
	}
	verbose("handler exits")
}

// NewSession returns a New session with defaults set.
// TODO: should session be a separate package.
func NewSession(port9p, cmd string, args ...string) *Session {
	return &Session{msize: 8192, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, port9p: port9p, cmd: cmd, args: args}
}

// NewSSHServer starts up a server for a cpu server.
func New(publicKeyFile, hostKeyFile string) (*ssh.Server, error) {
	v("configure SSH server")
	publicKeyOption := func(ctx ssh.Context, key ssh.PublicKey) bool {
		data, err := ioutil.ReadFile(publicKeyFile)
		if err != nil {
			fmt.Print(err)
			return false
		}
		allowed, _, _, _, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			fmt.Print(err)
			return false
		}
		return ssh.KeysEqual(key, allowed)
	}

	// Now we run as an ssh server, and each time we get a connection,
	// we run that command after setting things up for it.
	forwardHandler := &ssh.ForwardedTCPHandler{}
	server := &ssh.Server{
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			log.Println("CPUD:Accepted forward", dhost, dport)
			return true
		}),
		// Pick a reasonable default, which can be used for a call to listen and which
		// will be overridden later from a listen.Addr
		Addr:             ":23",
		PublicKeyHandler: publicKeyOption,
		ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
			log.Println("CPUD:attempt to bind", host, port, "granted")
			return true
		}),
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        forwardHandler.HandleSSHRequest,
			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
		},
		Handler: handler,
	}

	if err := server.SetOption(ssh.HostKeyFile(hostKeyFile)); err != nil {
		// We don't much care about this, what's the right thing to do here?
		// just printint from this function is kind of icky.
	}
	return server, nil
}
