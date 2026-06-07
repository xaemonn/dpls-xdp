package ebpf

import (
	"fmt"
	"log"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

var cgroupLink link.Link

// AttachCgroup attaches the dpls_cgroup_connect4 program to the root cgroup.
func AttachCgroup() error {
	if sendmsg4Prog == nil {
		log.Printf("[eBPF Loader] No cgroup program loaded, skipping cgroup attach")
		return nil
	}

	// In Ubuntu 24.04, the cgroup v2 mount is usually at /sys/fs/cgroup
	cgroupPath := "/sys/fs/cgroup"
	l, err := link.AttachCgroup(link.CgroupOptions{
		Path:    cgroupPath,
		Attach:  ebpf.AttachCGroupInet4Connect, // Must specify which cgroup hook this is
		Program: sendmsg4Prog,
	})
	if err != nil {
		return fmt.Errorf("[eBPF Loader] failed to attach cgroup program to %s: %w", cgroupPath, err)
	}

	cgroupLink = l
	log.Printf("[eBPF Loader] Successfully attached sender-side bypass hook to %s", cgroupPath)
	return nil
}

// DetachCgroup removes the cgroup program.
func DetachCgroup() error {
	if cgroupLink != nil {
		if err := cgroupLink.Close(); err != nil {
			log.Printf("[eBPF Loader] Warning: failed to detach cgroup link: %v", err)
		} else {
			log.Printf("[eBPF Loader] Successfully detached cgroup hook")
		}
		cgroupLink = nil
	}
	if sendmsg4Prog != nil {
		sendmsg4Prog.Close()
	}
	return nil
}
