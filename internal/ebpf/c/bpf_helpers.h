#ifndef __BPF_HELPERS_H
#define __BPF_HELPERS_H

typedef unsigned int __u32;
typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned long long __u64;

#define SEC(NAME) __attribute__((section(NAME), used))

// Helper for kernel tracing
#ifndef bpf_printk
#define bpf_printk(fmt, ...) \
    ({ \
        char ____fmt[] = fmt; \
        bpf_trace_printk(____fmt, sizeof(____fmt), ##__VA_ARGS__); \
    })
#endif

// BPF helper function signatures mapped to standard kernel helper IDs
static void *(*bpf_map_lookup_elem)(void *map, const void *key) = (void *) 1;
static long (*bpf_map_update_elem)(void *map, const void *key, const void *value, __u64 flags) = (void *) 2;
static long (*bpf_map_delete_elem)(void *map, const void *key) = (void *) 3;
static long (*bpf_trace_printk)(const char *fmt, __u32 fmt_size, ...) = (void *) 6;

#endif /* __BPF_HELPERS_H */
