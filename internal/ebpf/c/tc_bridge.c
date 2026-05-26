#include "bpf_helpers.h"

// Define network structures manually to ensure compilation without local headers dependency
struct ethhdr {
	unsigned char h_dest[6];
	unsigned char h_source[6];
	unsigned short h_proto;
};

struct iphdr {
	unsigned char ihl:4, version:4;
	unsigned char tos;
	unsigned short tot_len;
	unsigned short id;
	unsigned short frag_off;
	unsigned char ttl;
	unsigned char protocol;
	unsigned short check;
	unsigned int saddr;
	unsigned int daddr;
};

struct udphdr {
	unsigned short source;
	unsigned short dest;
	unsigned short len;
	unsigned short check;
};

// Lightweight __sk_buff definition matching the Linux kernel format
struct __sk_buff {
	__u32 len;
	__u32 pkt_type;
	__u32 mark;
	__u32 queue_mapping;
	__u32 protocol;
	__u32 vlan_present;
	__u32 vlan_tci;
	__u32 vlan_proto;
	__u32 priority;
	__u32 ingress_ifindex;
	__u32 ifindex;
	__u32 tc_index;
	__u32 cb[5];
	__u32 hash;
	__u32 tc_classid;
	__u32 data;
	__u32 data_end;
};

// C representation of DependencyRule matching Go schemas.go
struct dependency_rule {
	__u32 subtask_id;
	__u32 ref_count;
	__u32 dest_ips[4]; // supports up to 4 destination IPs
};

// Standard bpf_map_def structure
struct bpf_map_def {
	unsigned int type;
	unsigned int key_size;
	unsigned int value_size;
	unsigned int max_entries;
	unsigned int map_flags;
};

// Maps defined in SEC("maps") for loader resolution
struct bpf_map_def SEC("maps") vault_map = {
	.type = 1, // BPF_MAP_TYPE_HASH
	.key_size = sizeof(__u32),
	.value_size = sizeof(struct dependency_rule),
	.max_entries = 1024,
};

struct bpf_map_def SEC("maps") retention_map = {
	.type = 9, // BPF_MAP_TYPE_LRU_HASH
	.key_size = sizeof(__u32),
	.value_size = 128, // caches up to 128 bytes of payload
	.max_entries = 512,
};

// TC Action codes
#define TC_ACT_OK 0
#define TC_ACT_SHOT 2

#define htons(value) ((__u16)((((__u16)(value) & 0xff00) >> 8) | (((__u16)(value) & 0x00ff) << 8)))

SEC("tc")
int handle_tc_ingress(struct __sk_buff *skb) {
	void *data = (void *)(long)skb->data;
	void *data_end = (void *)(long)skb->data_end;

	// Check boundary for Ethernet header
	struct ethhdr *eth = data;
	if ((void *)(eth + 1) > data_end) {
		return TC_ACT_OK;
	}

	// Verify protocol is IPv4 (0x0800)
	if (eth->h_proto != htons(0x0800)) {
		return TC_ACT_OK;
	}

	// Parse IP header
	struct iphdr *ip = (void *)(eth + 1);
	if ((void *)(ip + 1) > data_end) {
		return TC_ACT_OK;
	}

	// Verify protocol is UDP (17)
	if (ip->protocol != 17) {
		return TC_ACT_OK;
	}

	// Parse UDP header
	struct udphdr *udp = (void *)((__u8 *)ip + (ip->ihl * 4));
	if ((void *)(udp + 1) > data_end) {
		return TC_ACT_OK;
	}

	// Intercept target UDP port 9000
	if (udp->dest == htons(9000)) {
		// Read payload location
		__u32 *subtask_id_ptr = (void *)(udp + 1);
		if ((void *)(subtask_id_ptr + 1) > data_end) {
			return TC_ACT_OK;
		}

		__u32 subtask_id = *subtask_id_ptr;

		// Perform lookup in vault_map
		struct dependency_rule *rule = bpf_map_lookup_elem(&vault_map, &subtask_id);
		if (rule) {
			bpf_printk("Intercepted Task %d, bypassed network!\n", subtask_id);

			// Cache first 128 bytes of data payload in retention_map
			__u8 *payload_src = (void *)(subtask_id_ptr);
			if ((void *)(payload_src + 128) <= data_end) {
				bpf_map_update_elem(&retention_map, &subtask_id, payload_src, 0);
			} else {
				// Cache partial data if smaller than 128 bytes
				__u8 temp_buf[128] = {0};
				__u32 len = (__u32)((__u8 *)data_end - payload_src);
				if (len < 128) {
					// Safe unrolled copy to prevent verifier errors
					for (__u32 i = 0; i < 128; i++) {
						if (i < len) {
							temp_buf[i] = payload_src[i];
						}
					}
					bpf_map_update_elem(&retention_map, &subtask_id, temp_buf, 0);
				}
			}
			
			// Bypass network delivery by dropping/redirecting if needed
			// For testing loop, return ACT_OK to allow normal delivery trace
			return TC_ACT_OK;
		}
	}

	return TC_ACT_OK;
}
