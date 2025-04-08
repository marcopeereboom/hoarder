package hoarder

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"k8s.io/utils/mount"
)

func NonVirtualMounts() ([]mount.MountPoint, error) {
	var validFS map[string]struct{} = map[string]struct{}{}
	f, err := os.Open("/proc/filesystems")
	if err != nil {
		return nil, fmt.Errorf("non virtual mounts: %w")
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(l, "nodev") || strings.Contains(l, "squashfs") {
			// skip obvious shit
			continue
		}
		validFS[strings.TrimSpace(l)] = struct{}{}
	}

	mounter := mount.New("")
	mounts, err := mounter.List()
	if err != nil {
		return nil, err
	}

	nonVirtualMounts := make([]mount.MountPoint, 0, 4)
	devices := map[string]struct{}{}
	for _, m := range mounts {
		if _, ok := validFS[m.Type]; ok {
			if _, ok := devices[m.Device]; ok {
				// skip seen devices
				continue
			}
			devices[m.Device] = struct{}{}
			nonVirtualMounts = append(nonVirtualMounts, m)
		}
	}

	return nonVirtualMounts, nil
}
