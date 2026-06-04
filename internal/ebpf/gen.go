//go:build ignore
// +build ignore

// gen.go — bpf2go code generation directive.
//
// This file is excluded from normal builds (go:build ignore) and is only used
// by `go generate` to regenerate the Go bindings for the TC-BPF C program.
//
// WHAT bpf2go DOES:
//   It compiles tc_bridge.c using clang, extracts the BPF map and program
//   definitions from the ELF output, and generates type-safe Go wrappers.
//   This eliminates manual unsafe.Pointer casts in loader_linux.go.
//
// HOW TO RUN (on Ubuntu 22.04 with Phase 1+2 setup complete):
//   cd ~/dpls-xdp
//   go generate ./internal/ebpf/
//
// This will create:
//   internal/ebpf/tc_bpfeb.go   — big-endian (for SPARC/MIPS)
//   internal/ebpf/tc_bpfel.go   — little-endian (for amd64/arm64) ← we use this
//   internal/ebpf/tc_bpfeb.o    — compiled BPF ELF (big-endian)
//   internal/ebpf/tc_bpfel.o    — compiled BPF ELF (little-endian) ← we use this
//
// The generated tc_bpfel.go will contain:
//   - loadTC()                 → loads the ELF into kernel
//   - tcObjects{}              → struct holding all map + program handles
//   - tcMaps{}                 → { VaultMap *ebpf.Map, RetentionMap *ebpf.Map }
//   - tcPrograms{}             → { TcIngress *ebpf.Program, TcEgress *ebpf.Program }
//
// After generation, loader_linux.go can use these generated types instead of
// the manual LoadCollectionSpec() approach, giving full type safety.
//
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang \
//   -cflags "-O2 -g -target bpf -I /usr/include/x86_64-linux-gnu" \
//   TC ./c/tc_bridge.c

package ebpf
