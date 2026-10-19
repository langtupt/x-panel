//go:build !linux

package files

import (
	"fmt"
	"os"
)

// HelperMain is only implemented on Linux.
func HelperMain([]string) {
	fmt.Fprintln(os.Stderr, "fshelper: only supported on linux")
	os.Exit(1)
}
