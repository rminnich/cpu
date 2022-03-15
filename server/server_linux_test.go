package server

import (
	"os"
	"os/exec"
	"testing"
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

// Now the fun begins. We have to be a demon.n
func TestDaemon(t *testing.T) {
	v = t.Logf

}
