module dpls-xdp

go 1.22.0

require (
	// cilium/ebpf: the Go library for loading BPF programs, creating maps,
	// and writing into kernel BPF maps via bpf() syscalls.
	// Used by loader_linux.go's WriteDependencyRuleToKernel().
	// Docs: https://pkg.go.dev/github.com/cilium/ebpf
	github.com/cilium/ebpf v0.13.2

	// golang.org/x/sys: provides unix syscall constants (unix.SOCK_CLOEXEC etc.)
	// Required by cilium/ebpf for BPF syscall wrappers.
	golang.org/x/sys v0.18.0
)

require golang.org/x/exp v0.0.0-20230224173230-c95f2b4c22f2 // indirect

// Note: run `go mod tidy` on Linux after cloning to populate go.sum.
// The go.sum file is generated from the module download checksums.
