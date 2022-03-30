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
	// log.Printf("os.Args %v, args %v", os.Args, args)
	flag.Parse()
	os.Exit(m.Run())
}
