package server

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateNameSpace(t *testing.T) {
	v = t.Logf
	s, err := New()
	if err != nil {
		t.Fatalf("New(): %v != nil", err)
	}
	d := t.TempDir()
	f := filepath.Join(d, "x")
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	s.Stdin, s.Stdout, s.Stderr = nil, o, e
	cmd := []string{"bash", "-c", fmt.Sprintf("mkdir -p %s && touch %s && ls %s", d, f, f)}
	if err := s.Remote(":0", cmd...); err != nil {
		t.Fatalf(`s.Remote(%q, 0): %v != nil`, cmd, err)
	}
	t.Logf("%q %q", o, e)
	want := f + "\n"
	if o.String() != want {
		t.Errorf("command output: %q != %q", o.String(), want)
	}
	if e.String() != "" {
		t.Errorf("command error: %q != %q", e.String(), "")
	}
	if _, err := os.Stat(f); err == nil {
		t.Errorf("os.Stat(%q): nil != %v", f, fs.ErrNotExist)
	}

}

// Now the fun begins. We have to be a demon.n
func TestDaemon(t *testing.T) {
	v = t.Logf
	s, err := New()
	if err != nil {
		t.Fatalf("New(): %v != nil", err)
	}
	d := t.TempDir()
	f := filepath.Join(d, "x")
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	s.Stdin, s.Stdout, s.Stderr = nil, o, e
	cmd := []string{"bash", "-c", fmt.Sprintf("mkdir -p %s && touch %s && ls %s", d, f, f)}
	if err := s.Remote(":0", cmd...); err != nil {
		t.Fatalf(`s.Remote(%q, 0): %v != nil`, cmd, err)
	}
	t.Logf("%q %q", o, e)
	want := f + "\n"
	if o.String() != want {
		t.Errorf("command output: %q != %q", o.String(), want)
	}
	if e.String() != "" {
		t.Errorf("command error: %q != %q", e.String(), "")
	}
	if _, err := os.Stat(f); err == nil {
		t.Errorf("os.Stat(%q): nil != %v", f, fs.ErrNotExist)
	}

}
