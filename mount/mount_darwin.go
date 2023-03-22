// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mount

import (
	"os"
)

// This mounter type may be useful should we need more tests: we can call mount with a mock
// mounter.
type mounter func(source string, target string, fstype string, flags uintptr, data string) error

// Mount takes a full fstab as a string and does whatever mounts are needed.
// It ignores comment lines, and lines with less than 6 fields. In principal,
// Mount should be able to do a full remount with the contents of /proc/mounts.
// Mount makes a best-case effort to mount the mounts passed in a
// string formatted to the fstab standard.  Callers should not die on
// a returned error, but be left in a situation in which further
// diagnostics are possible.  i.e., follow the "Boots not Bricks"
// principle.
func Mount(fstab string) error {
	return os.ErrInvalid
}
