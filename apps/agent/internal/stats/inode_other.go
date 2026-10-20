//go:build !linux

package stats

import "os"

// Development builds only: rotation is detected by size alone.
func inodeOf(os.FileInfo) uint64 { return 0 }
