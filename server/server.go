// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"fmt"
	"io"
	"log"
	"net"
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

// Server is an instance of a cpu server
type Server struct {
	addr       string // Addr is an address, see net.Dial
	port       string
	publicKey  []byte
	hostKeyPEM []byte
	ssh        ssh.Server
}

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
	wtf    string
	msize  int
	mopts  string
	port9p string
	cmd    string
	args   []string
}

// a nonce is a [32]byte containing only printable characters, suitable for use as a string
type nonce [32]byte

var (
	pid1 bool
	v    = log.Printf // func(string, ...interface{}) {}
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

// Remote starts up a remote cpu session. It is started by a cpu
// daemon via a -remote switch.
// This code assumes that cpud is running as init, or that
// an init has started a cpud, and that the code is running
// with a private namespace (CLONE_NEWS on Linux; RFNAMEG on Plan9).
// On Linux, it starts as uid 0, and once the mount/bind is done,
// calls DropPrivs.
func (s *Session) Run() error {
	var errors error
	if err := unix.Mount("cpu", "/tmp", "tmpfs", 0, ""); err != nil {
		log.Fatalf(`unix.Mount("cpu", "/tmp", "tmpfs", 0, ""); %v != nil`, err)
	}
	// N.B. if the namespace variable is set,
	// even if it is empty, server will try to do
	// the 9p mount.
	if b, ok := os.LookupEnv("CPU_NAMESPACE"); ok {
		binds, err := ParseBinds(b)
		if err != nil {
			s.fail = true
			err = multierror.Append(errors, err)
		}
		s.binds = binds
		if err := s.TmpMounts(); err != nil {
			s.fail = true
			errors = multierror.Append(err)
		}
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
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
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
			io.Copy(f, s) // stdin
		}()
		io.Copy(s, f) // stdout
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
			s.Exit(cmd.ProcessState.ExitCode())
		}

	} else {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
		v("running command without pty")
		if err := cmd.Run(); errval(err) != nil {
			v("CPUD:err %v", err)
			s.Exit(1)
		}
	}
	verbose("handler exits")
}

// New returns a New server with defaults set.
func New() *Server {
	return &Server{port: "23"}
}

// New returns a New session with defaults set.
// TODO: should session be a separate package.
func NewSession(port9p, cmd string, args ...string) *Session {
	return &Session{msize: 8192, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, port9p: port9p, cmd: cmd, args: args}
}

// WithPort sets the server port, i.e. an ssh server port.
func (s *Server) WithPort(port string) *Server {
	s.port = port
	return s
}

// WithPublicKey sets the server public key (e.g. ~/.ssh/id_rsa.pub)
func (s *Server) WithPublicKey(key []byte) *Server {
	s.publicKey = key
	return s
}

// WithHostKeyPEM sets the server HostKeyPEM
func (s *Server) WithHostKeyPEM(key []byte) *Server {
	s.hostKeyPEM = key
	return s
}

// WithAddr sets the server listen address.
func (s *Server) WithAddr(addr string) *Server {
	s.addr = addr
	return s
}

// func doInit() error {
// 	if pid1 {
// 		if err := cpuSetup(); err != nil {
// 			log.Printf("CPUD:CPU setup error with cpu running as init: %v", err)
// 		}
// 		cmds := [][]string{{"/bin/sh"}, {"/bbin/dhclient", "-v"}}
// 		verbose("Try to run %v", cmds)

// 		for _, v := range cmds {
// 			verbose("Let's try to run %v", v)
// 			if _, err := os.Stat(v[0]); os.IsNotExist(err) {
// 				verbose("it's not there")
// 				continue
// 			}

// 			// I *love* special cases. Evaluate just the top-most symlink.
// 			//
// 			// In source mode, this would be a symlink like
// 			// /buildbin/defaultsh -> /buildbin/elvish ->
// 			// /buildbin/installcommand.
// 			//
// 			// To actually get the command to build, argv[0] has to end
// 			// with /elvish, so we resolve one level of symlink.
// 			if filepath.Base(v[0]) == "defaultsh" {
// 				s, err := os.Readlink(v[0])
// 				if err == nil {
// 					v[0] = s
// 				}
// 				verbose("readlink of %v returns %v", v[0], s)
// 				// and, well, it might be a relative link.
// 				// We must go deeper.
// 				d, b := filepath.Split(v[0])
// 				d = filepath.Base(d)
// 				v[0] = filepath.Join("/", os.Getenv("UROOT_ROOT"), d, b)
// 				verbose("is now %v", v[0])
// 			}

// 			cmd := exec.Command(v[0], v[1:]...)
// 			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
// 			cmd.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true}
// 			verbose("Run %v", cmd)
// 			if err := cmd.Start(); err != nil {
// 				verbose("CPUD:Error starting %v: %v", v, err)
// 				continue
// 			}
// 		}
// 	}
// 	verbose("Kicked off startup jobs, now serve ssh")
// 	publicKeyOption := func(ctx ssh.Context, key ssh.PublicKey) bool {
// 		// Glob the users's home directory for all the
// 		// possible keys?
// 		data, err := ioutil.ReadFile(*pubKeyFile)
// 		if err != nil {
// 			fmt.Print(err)
// 			return false
// 		}
// 		allowed, _, _, _, _ := ssh.ParseAuthorizedKey(data)
// 		return ssh.KeysEqual(key, allowed)
// 	}

// 	// Now we run as an ssh server, and each time we get a connection,
// 	// we run that command after setting things up for it.
// 	forwardHandler := &ssh.ForwardedTCPHandler{}
// 	server := ssh.Server{
// 		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
// 			log.Println("CPUD:Accepted forward", dhost, dport)
// 			return true
// 		}),
// 		Addr:             ":" + *port,
// 		PublicKeyHandler: publicKeyOption,
// 		ReversePortForwardingCallback: ssh.ReversePortForwardingCallback(func(ctx ssh.Context, host string, port uint32) bool {
// 			log.Println("CPUD:attempt to bind", host, port, "granted")
// 			return true
// 		}),
// 		RequestHandlers: map[string]ssh.RequestHandler{
// 			"tcpip-forward":        forwardHandler.HandleSSHRequest,
// 			"cancel-tcpip-forward": forwardHandler.HandleSSHRequest,
// 		},
// 		Handler: handler,
// 	}

// 	// start the process reaper
// 	procs := make(chan uint)
// 	verbose("Start the process reaper")
// 	go cpuDone(procs)

// 	server.SetOption(ssh.HostKeyFile(*hostKeyFile))
// 	log.Println("CPUD:starting ssh server on port " + *port)
// 	if err := server.ListenAndServe(); err != nil {
// 		log.Printf("CPUD:err %v", err)
// 	}
// 	verbose("server.ListenAndServer returned")

// 	numprocs := <-procs
// 	verbose("Reaped %d procs", numprocs)
// 	return nil
// }

// // TODO: we've been tryinmg to figure out the right way to do usage for years.
// // If this is a good way, it belongs in the uroot package.
// func usage() {
// 	var b bytes.Buffer
// 	flag.CommandLine.SetOutput(&b)
// 	flag.PrintDefaults()
// 	log.Fatalf("Usage: cpu [options] host [shell command]:\n%v", b.String())
// }

// func main() {
// 	verbose("Args %v pid %d *runasinit %v *remote %v", os.Args, os.Getpid(), *runAsInit, *remote)
// 	args := flag.Args()
// 	switch {
// 	case *runAsInit:
// 		verbose("Running as Init")
// 		if err := doInit(); err != nil {
// 			log.Fatalf("CPUD(as init):%v", err)
// 		}
// 	case *remote:
// 		verbose("Running as remote")
// 		if err := runRemote(strings.Join(args, " "), *port9p); err != nil {
// 			log.Fatalf("CPUD(as remote):%v", err)
// 		}
// 	default:
// 		log.Fatal("CPUD:can only run as remote or pid 1")
// 	}
// }

// Listen returns a net.Listener for a Server.
func (s *Server) Listen() (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(s.addr, s.port))
}

// SSHConfig configures a Server to serve SSH sessions.
// ready to serve. starts a synchronous daemon, i.e. blocks until
// it is done.
// Assumptions:
// This is not init. It is started by init. It will not reap zombies.
// It will not create devices and common mounts (e.g. /proc)
func (s *Server) SSHConfig() *Server {
	v("configure SSH server")
	publicKeyOption := func(ctx ssh.Context, key ssh.PublicKey) bool {
		allowed, _, _, _, _ := ssh.ParseAuthorizedKey(s.publicKey)
		return ssh.KeysEqual(key, allowed)
	}

	// Now we run as an ssh server, and each time we get a connection,
	// we run that command after setting things up for it.
	forwardHandler := &ssh.ForwardedTCPHandler{}
	server := ssh.Server{
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			log.Println("CPUD:Accepted forward", dhost, dport)
			return true
		}),
		Addr:             s.addr + ":" + s.port,
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

	server.SetOption(ssh.HostKeyPEM(s.hostKeyPEM))

	s.ssh = server
	return s
}

// Serve serves cpu sessions. It is the daemon mode: it does not do all that
// init mode does, in particular it is not a process reaper, but it does
// accept connections and start an instance of itself for each individual
// connection. Per-connection startup is done in the handler.
func (s *Server) Serve(ln net.Listener) error {
	log.Println("CPUD:starting ssh server on port " + s.port)
	if err := s.ssh.Serve(ln); err != nil {
		log.Printf("CPUD:err %v", err)
	}
	verbose("server.ListenAndServer returned")
	return nil
}

// Close closes a CPU session. It does not terminate any active
// session, but it will stop any new connections.
func (s *Server) Close() error {
	v("CPUD: closing SSH session")
	return s.ssh.Close()
}
