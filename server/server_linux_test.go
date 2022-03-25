package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	config "github.com/kevinburke/ssh_config"
	"github.com/u-root/cpu/client"
)

func TestHelperProcess(t *testing.T) {
	v, ok := os.LookupEnv("GO_WANT_HELPER_PROCESS")
	if !ok {
		t.Logf("just a helper")
		return
	}
	// See if the directory exists. If it does, that's an error
	if _, err := os.Stat(v); err == nil {
		os.Exit(1)
	}

}

// TestPrivateNameSpace tests if we are privatizing mounts
// correctly. Because the private tmp mount is not a given,
// i.e. it only happens if we have a CPU_NAMESPACE,
// this test further does a tmpfs mount.
func TestPrivateNameSpace(t *testing.T) {
	d := t.TempDir()
	t.Logf("Call helper %q", os.Args[0])
	c := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
	c.Env = []string{"GO_WANT_HELPER_PROCESS=" + d}
	o, err := c.CombinedOutput()
	t.Logf("out %s", o)
	if err != nil {
		//		exitErr, ok := err.(*exec.ExitError)
		//	if !ok {
		t.Errorf("Error: %v", err)

		//		}
		//		retCode = exitErr.Sys().(syscall.WaitStatus).ExitStatus()
	}

}

// Now the fun begins. We have to be a demon.
func TestDaemon(t *testing.T) {
	runtime.GOMAXPROCS(1)
	v = t.Logf
	d := t.TempDir()
	t.Logf("tempdir is %q", d)
	if err := os.Setenv("HOME", d); err != nil {
		t.Fatalf(`os.Setenv("HOME", %s): %v != nil`, d, err)
	}
	// https://github.com/kevinburke/ssh_config/issues/2
	hackconfig := fmt.Sprintf(string(sshConfig), filepath.Join(d, ".ssh"))
	if err := gendotssh(d, hackconfig); err != nil {
		t.Fatalf(`gendotssh(%s): %v != nil`, d, err)
	}

	v = t.Logf
	s := New().WithPort("").WithPublicKey(publicKey).WithHostKeyPEM(hostKey).WithAddr("localhost").SSHConfig()

	ln, err := s.Listen()
	if err != nil {
		t.Fatalf("s.Listen(): %v != nil", err)
	}
	t.Logf("Listening on %v", ln.Addr())
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// this is a racy test.
	// The ssh package really ought to allow you to accept
	// on a socket and then call with that socket. This would be
	// more in line with bsd sockets which let you write a server
	// and client in line, e.g.
	// socket/bind/listen/connect/accept
	// oh well.
	go func(t*testing.T) {
		if err := s.Serve(ln); err != nil {
			t.Errorf("s.Daemon(): %v != nil", err)
		}
	}(t)
	v = t.Logf
	// From this test forward, at least try to get a port.
	// For this test, there must be a key.
	// hack for lack in ssh_config
	// https://github.com/kevinburke/ssh_config/issues/2
	cfg, err := config.Decode(bytes.NewBuffer([]byte(hackconfig)))
	if err != nil {
		t.Fatal(err)
	}
	host, err := cfg.Get("server", "HostName")
	if err != nil || len(host) == 0 {
		t.Fatalf(`cfg.Get("server", "HostName"): (%q, %v) != (localhost, nil`, host, err)
	}
	kf, err := cfg.Get("server", "IdentityFile")
	if err != nil || len(kf) == 0 {
		t.Fatalf(`cfg.Get("server", "IdentityFile"): (%q, %v) != (afilename, nil`, kf, err)
	}
	t.Logf("HostName %q, IdentityFile %q", host, kf)
	c := client.Command(host, "ls", "-l").WithPrivateKeyFile(kf).WithPort(port).WithRoot("/").WithNameSpace("")
	if err := c.Dial(); err != nil {
		t.Fatalf("Dial: got %v, want nil", err)
	}
	if err = c.Start(); err != nil {
		t.Fatalf("Start: got %v, want nil", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatalf("Close: got %v, want nil", err)
		}
	}()
	if err := c.Stdin.Close(); err != nil && !errors.Is(err, io.EOF) {
		t.Errorf("Close stdin: Got %v, want nil", err)
	}
	if err := c.Wait(); err != nil {
		t.Fatalf("Wait: got %v, want nil", err)
	}
	r, err := c.Outputs()
	if err != nil {
		t.Errorf("Outputs: got %v, want nil", err)
	}
	t.Logf("c.Run: (%v, %q, %q)", err, r[0].String(), r[1].String())
}
