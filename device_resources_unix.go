//go:build linux || darwin

package licentivo

import "syscall"

func platformDisplayResources() (int64, int64) {
	var info syscall.Statfs_t
	if syscall.Statfs("/", &info) != nil {
		return 0, 0
	}
	return 0, int64(uint64(info.Bsize) * info.Blocks / (1024 * 1024 * 1024))
}
