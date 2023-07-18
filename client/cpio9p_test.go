// Copyright 2023 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"io/ioutil"
	"path/filepath"
	"testing"
)

func TestCPIO9P(t *testing.T) {
	d := t.TempDir()
	bogus := filepath.Join(d, "bogus")
	if _, err := NewCPIO9P(bogus); err == nil {
		t.Errorf("Opening non-existent file: got nil, want err")
	}
	if err := ioutil.WriteFile(bogus, []byte("bogus"), 0666); err != nil {
		t.Fatal(err)
	}

	v = t.Logf
	if _, err := NewCPIO9P(bogus); err == nil {
		t.Errorf("Opening bad file: got nil, want err")
	}

	fs, err := NewCPIO9P("data/a.cpio")
	if err != nil {
		t.Fatalf("data/a.cpio: got %v, want nil", err)
	}

	// See if anything is there.
	root, err := fs.Attach()
	if err != nil {
		t.Fatalf("Attach: got %v, want nil", err)
	}
	t.Logf("root:%v",root)

	if q, f, err := root.Walk([]string{"barf"}); err == nil {
		t.Errorf("walking 'barf': want err, got (%v,%v,%v)", q, f, err)
	}

	if _, _, err := root.Walk([]string{"b"}); err != nil {
		t.Errorf("walking 'b': want nil, got %v", err)
	}

	q, f, err := root.Walk([]string{"b", "c"})
	if err != nil {
		t.Fatalf("walking a/b: want nil, got %v", err)
	}
	if len(q) != 2 {
		t.Fatalf("walking a/b: want 2 qids, got (%v,%v)", q, err)
	}
	if f == nil {
		t.Fatalf("walking a/b: want non-nil file, got nil")
	}
			
		

}
