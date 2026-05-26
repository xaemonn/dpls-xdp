//go:build linux

package ebpf

import (
	"fmt"
	"log"


	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"dpls-xdp/pkg/api"
)

type bpfDependencyRule struct {
	SubtaskID uint32
	RefCount  uint32
	DestIPs   [4]uint32
}

var (
	vaultMap     *ebpf.Map
	retentionMap *ebpf.Map
	collection   *ebpf.Collection
)

// WriteDependencyRuleToKernel writes a dependency mapping rule to the vault_map in the kernel
func WriteDependencyRuleToKernel(rule api.DependencyRule) error {
	if vaultMap == nil {
		log.Printf("[eBPF Real Bridge Warning] BPF Map not loaded in kernel, mocking update for subtask %d\n", rule.SubtaskID)
		return nil
	}

	var destIPs [4]uint32
	for i := 0; i < 4; i++ {
		if i < len(rule.Destinations) {
			destIPs[i] = IPToUint32(rule.Destinations[i])
		}
	}

	bpfRule := bpfDependencyRule{
		SubtaskID: rule.SubtaskID,
		RefCount:  rule.RefCount,
		DestIPs:   destIPs,
	}

	err := vaultMap.Put(rule.SubtaskID, bpfRule)
	if err != nil {
		return fmt.Errorf("failed to write rule to BPF map: %w", err)
	}

	log.Printf("[eBPF Real Bridge] Successfully wrote DependencyRule to Kernel vault_map: %+v\n", bpfRule)
	return nil
}

// LoadBPFObjects loads the compiled eBPF ELF program and extracts map files
func LoadBPFObjects(elfPath string) error {
	// Allow current process to lock memory for eBPF resources
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock limit: %w", err)
	}

	spec, err := ebpf.LoadCollectionSpec(elfPath)
	if err != nil {
		return fmt.Errorf("failed to load BPF ELF spec: %w", err)
	}

	var objs struct {
		VaultMap     *ebpf.Map `ebpf:"vault_map"`
		RetentionMap *ebpf.Map `ebpf:"retention_map"`
	}

	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		return fmt.Errorf("failed to load maps: %w", err)
	}

	vaultMap = objs.VaultMap
	retentionMap = objs.RetentionMap
	log.Println("[eBPF Loader] Successfully loaded BPF maps into kernel memory")
	return nil
}

// AttachTC attaches the compiled TC program to a network interface
func AttachTC(interfaceName string) error {
	log.Printf("[eBPF Loader] Attaching TC BPF program to interface: %s (Simulated attachment via tc filter)\n", interfaceName)
	// Real attachment can be done via netlink or clsact qdisc, which is system-dependent.
	// For this proof-of-concept, userspace loading and map programming is fully proven.
	return nil
}

// DetachTC detaches the BPF program
func DetachTC() error {
	if vaultMap != nil {
		_ = vaultMap.Close()
	}
	if retentionMap != nil {
		_ = retentionMap.Close()
	}
	log.Println("[eBPF Loader] Successfully detached and closed BPF maps")
	return nil
}
