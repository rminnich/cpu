// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"bytes"
	"net"
	"reflect"
	"testing"
	"time"

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
				Bind{Local: "/bin", Remote: "/bin"},
			},
		},
		{
			in: "/bin", out: []Bind{
				Bind{Local: "/bin", Remote: "/bin"},
			},
		},

		{
			in: "/bin=/home/user/bin",
			out: []Bind{
				Bind{Local: "/bin", Remote: "/home/user/bin"},
			},
		},
		{
			in: "/bin=/home/user/bin:/home",
			out: []Bind{
				Bind{Local: "/bin", Remote: "/home/user/bin"},
				Bind{Local: "/home", Remote: "/home"},
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
	s := New()
	t.Logf("New server: %v", s)
}

// Not sure testing this is a great idea but ... it works so ...
func TestDropPrivs(t *testing.T) {
	s := New()
	if err := s.DropPrivs(); err != nil {
		t.Fatalf("s.DropPrivs(): %v != nil", err)
	}
}

func TestRemoteNoNameSpace(t *testing.T) {
	v = t.Logf
	s := New()
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	s.Stdin, s.Stdout, s.Stderr = nil, o, e
	if err := s.Remote(":0", "echo", "hi"); err != nil {
		t.Fatalf(`s.Remote("echo hi", 0): %v != nil`, err)
	}
	t.Logf("%q %q", o, e)
	if o.String() != "hi\n" {
		t.Errorf("command output: %q != %q", o.String(), "hi\n")
	}
	if e.String() != "" {
		t.Errorf("command error: %q != %q", e.String(), "")
	}
}

func TestDaemonStart(t *testing.T) {
	v = t.Logf
	s := New().WithPort("").WithPublicKey(publicKey).WithHostKeyPEM(hostKey).WithAddr("localhost")

	ln, err := s.Listen()
	if err != nil {
		t.Fatalf("s.Listen(): %v != nil", err)
	}
	t.Logf("Listening on %v", ln.Addr())
	// this is a racy test.
	go func() {
		time.Sleep(5 * time.Second)
		s.Close()
	}()
	if err := s.Serve(ln); err != nil {
		t.Fatalf("s.Daemon(): %v != nil", err)
	}
	t.Logf("Daemon returns")

}

// TestDaemonConnect tests connecting to a daemon and exercising
// minimal operations.
func TestDaemonConnect(t *testing.T) {
	v = t.Logf
	s := New().WithPort("").WithPublicKey(publicKey).WithHostKeyPEM(hostKey).WithAddr("localhost")

	ln, err := s.Listen()
	if err != nil {
		t.Fatalf("s.Listen(): %v != nil", err)
	}
	t.Logf("Listening on %v", ln.Addr())
	host, port, err := net.SplitHostPort(ln.Addr().String())
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
	go func() {
		if err := s.Serve(ln); err != nil {
			t.Fatalf("s.Daemon(): %v != nil", err)
		}
	}()
	v = t.Logf
	// From this test forward, at least try to get a port.
	// For this test, there must be a key.

	c := client.Command(h, "ls", "-l").WithPort(port).WithRoot("/").WithNameSpace("")
	if err := c.Dial(); err != nil {
		t.Fatalf("Dial: got %v, want nil", err)
	}
	if err = c.Start(); err != nil {
		t.Fatalf("Start: got %v, want nil", err)
	}
	if err = c.SetupInteractive(); err != nil {
		t.Fatalf("SetupInteractive: got %v, want nil", err)
	}
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
	if err := c.Close(); err != nil {
		t.Fatalf("Close: got %v, want nil", err)
	}
}
