// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	config "github.com/kevinburke/ssh_config"
	"github.com/u-root/cpu/client"
)

func TestParseBind(t *testing.T) {
	var tests = []struct {
		in    string
		out   []Bind
		error string
	}{
		{in: "", out: []Bind{}},
		{in: ":", out: []Bind{}, error: "bind: element 0 is zero length"},
		{in: "l=:", out: []Bind{}, error: "bind: element 0:name in \"l=\": zero-length remote name"},
		{in: "=r:", out: []Bind{}, error: "bind: element 0:name in \"=r\": zero-length local name"},
		{
			in: "/bin",
			out: []Bind{
				{Local: "/bin", Remote: "/bin"},
			},
		},
		{
			in: "/bin", out: []Bind{
				{Local: "/bin", Remote: "/bin"},
			},
		},

		{
			in: "/bin=/home/user/bin",
			out: []Bind{
				{Local: "/bin", Remote: "/home/user/bin"},
			},
		},
		{
			in: "/bin=/home/user/bin:/home",
			out: []Bind{
				{Local: "/bin", Remote: "/home/user/bin"},
				{Local: "/home", Remote: "/home"},
			},
		},
	}
	for i, tt := range tests {
		b, err := ParseBinds(tt.in)
		t.Logf("Test %d:%q => (%q, %v), want %q", i, tt.in, b, err, tt.out)
		if len(tt.error) == 0 {
			if err != nil {
				t.Errorf("%d:ParseBinds(%q): err %v != nil", i, tt.in, err)
				continue
			}
			if !reflect.DeepEqual(b, tt.out) {
				t.Errorf("%d:ParseBinds(%q): Binds %q != %q", i, tt.in, b, tt.out)
				continue
			}
			continue
		}
		if err == nil {
			t.Errorf("%d:ParseBinds(%q): err nil != %q", i, tt.in, tt.error)
			continue
		}
		if err.Error() != tt.error {
			t.Errorf("%d:ParseBinds(%q): err %s != %s", i, tt.in, err.Error(), tt.error)
			continue
		}

	}
}

func TestNewServer(t *testing.T) {
	s, err := New("", "")
	if err != nil {
		t.Fatalf(`New("", ""): %v != nil`, err)
	}
	t.Logf("New server: %v", s)
}

// Not sure testing this is a great idea but ... it works so ...
func TestDropPrivs(t *testing.T) {
	s := NewSession("", "/bin/true")
	if err := s.DropPrivs(); err != nil {
		t.Fatalf("s.DropPrivs(): %v != nil", err)
	}
}

func TestRemoteNoNameSpace(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skipf("Skipping as we are not root")
	}
	v = t.Logf
	s := NewSession("", "date")
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	s.Stdin, s.Stdout, s.Stderr = nil, o, e
	if err := s.Run(); err != nil {
		t.Fatalf(`s.Run("", "date"): %v != nil`, err)
	}
	t.Logf("%q %q", o, e)
	if len(o.String()) == 0 {
		t.Errorf("no command output: \"\" != non-zero-length string")
	}
	if e.String() != "" {
		t.Errorf("command error: %q != %q", e.String(), "")
	}
}

func gendotssh(dir, config string) (string, error) {
	dotssh := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(dotssh, 0700); err != nil {
		return "", err
	}

	// https://github.com/kevinburke/ssh_config/issues/2
	hackconfig := fmt.Sprintf(string(sshConfig), filepath.Join(dir, ".ssh"))
	for _, f := range []struct {
		name string
		val  []byte
	}{
		{name: "config", val: []byte(hackconfig)},
		{name: "hostkey", val: hostKey},
		{name: "server", val: privateKey},
		{name: "server.pub", val: publicKey},
	} {
		if err := ioutil.WriteFile(filepath.Join(dotssh, f.name), f.val, 0644); err != nil {
			return "", err
		}
	}
	return hackconfig, nil
}

func TestDaemonStart(t *testing.T) {
	v = t.Logf
	s, err := New("", "")
	if err != nil {
		t.Fatalf(`New("", ""): %v != nil`, err)
	}

	ln, err := net.Listen("tcp", "")
	if err != nil {
		t.Fatalf("net.Listen(): %v != nil", err)
	}
	t.Logf("Listening on %v", ln.Addr())
	// this is a racy test.
	go func() {
		time.Sleep(5 * time.Second)
		s.Close()
	}()

	if err := s.Serve(ln); err != ssh.ErrServerClosed {
		t.Fatalf("s.Daemon(): %v != %v", err, ssh.ErrServerClosed)
	}
	t.Logf("Daemon returns")
}

func TestDaemonConnectHelper(t *testing.T) {
	if _, ok := os.LookupEnv("GO_WANT_DAEMON_HELPER_PROCESS"); !ok {
		t.Logf("just a helper")
		return
	}
	t.Logf("As a helper, we are supposed to run %q", args)
	s := NewSession(port9p, args[0], args[1:]...)
	// Step through the things a server is supposed to do with a session
	if err := s.Run(); err != nil {
		log.Fatalf("CPUD(as remote):%v", err)
	}
}

// TestDaemonConnect tests connecting to a daemon and exercising
// minimal operations.
func TestDaemonConnect(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skipf("Skipping as we are not root")
	}
	d := t.TempDir()
	if err := os.Setenv("HOME", d); err != nil {
		t.Fatalf(`os.Setenv("HOME", %s): %v != nil`, d, err)
	}
	hackconfig, err := gendotssh(d, string(sshConfig))
	if err != nil {
		t.Fatalf(`gendotssh(%s): %v != nil`, d, err)
	}

	v = t.Logf
	s, err := New(filepath.Join(d, ".ssh", "server.pub"), filepath.Join(d, ".ssh", "hostkey"))
	if err != nil {
		t.Fatalf(`New(%q, %q): %v != nil`, filepath.Join(d, ".ssh", "server.pub"), filepath.Join(d, ".ssh", "hostkey"), err)
	}

	ln, err := net.Listen("tcp", "")
	if err != nil {
		t.Fatalf(`net.Listen("", ""): %v != nil`, err)
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
	go func(t *testing.T) {
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
	c := client.Command(host, "date").WithPrivateKeyFile(kf).WithPort(port).WithRoot("/").WithNameSpace("").WithCommand(os.Args[0] + " -test.run TestDaemonConnectHelper -test.v --")
	c.Env = append(c.Env, "GO_WANT_DAEMON_HELPER_PROCESS=1")
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
	if err := c.Stdin.Close(); err != nil {
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

var (
	args   []string
	port9p string
)

func TestMain(m *testing.M) {
	// Strip out the args after --
	x := -1
	var osargs = os.Args
	for i := range os.Args {
		if x > 0 {
			args = os.Args[i:]
			break
		}
		if os.Args[i] == "--" {
			osargs = os.Args[:i]
			x = i
		}
	}
	// Process any port9p directive
	if len(args) > 1 && args[0] == "-port9p" {
		port9p = args[1]
		args = args[2:]
	}
	os.Args = osargs
	log.Printf("os.Args %v, args %v", os.Args, args)
	flag.Parse()
	os.Exit(m.Run())
}
