# Chat Transcript: eDAG-MEC Thesis Project Development

**User:**
Here is a comprehensive summary of our discussion and your research project plan, which you can easily copy and paste to take forward into another chat or document...
[Provided summary of the "Brain and Muscle" eDAG-MEC project]
Take these two things and tell me how should i explain the novelty that we wanted to do

**AI:**
This is a highly ambitious systems engineering project. You are essentially taking a theoretical networking concept and forcing it to survive contact with reality in a Linux kernel. To effectively explain the novelty of this project, you need to frame it as solving the "Abstraction Gap" between application logic and network routing. 
[Detailed the Core Elevator Pitch, Flaws in the Status Quo, Brain and Muscle Architecture, and Measurable Impact]

**User:**
i want to support the novelty with actual papers as you can see in the pdf that is the research done by gpt to analyse different paper that has work upon eBPF or XDP or related technologies in edge computing
Its more like an research paper work rather than systems project so i need to keep the complexity lesser and maintain the novelty

**AI:**
Balancing high academic novelty with a manageable engineering scope is the holy grail of systems research.
[Grouped the literature into "Dumb but Fast" vs "Reactive", explained Proactive Cross-Layer Co-Design, and outlined a scoped implementation strategy using K3s and Cilium]

**User:**
[Uploaded base paper: Dependency-Aware Dynamic Priority Scheduling...]
this was the actual base paper for your reference

**AI:**
Having the actual baseline paper is incredibly helpful. Look at Section III.C (Transmission Model), Equation (2). The paper assumes that once data leaves Subtask A, it flows frictionlessly to Subtask B. In reality, traversing the Kubernetes stack destroys this. 
[Explained how to code "The Brain" using the paper's DPLS algorithm combined with an eBPF system call]

**User:**
see it is basically for my BTech project where we need to showcase this as a research paper and defend it against a panel... since the xdp paper never touched this layer, so if someone is combining the DPLS scheduling algorithm with eBPF/XDP data plane to actually showcase the latency reduction... will it be considered as a novel work? And on top of it we can add caching as well in this system since it is highly required

**AI:**
Yes, this is highly novel. There is virtually no literature explicitly combining application-aware DAG scheduling with kernel-bypass data planes. Adding caching elevates the novelty from "routing optimization" to a "Distributed Stateful Edge." 
[Explained how to repurpose eBPF maps as an ultra-low-latency cache for intermediate subtask outputs]

**User:**
[Uploaded Claude's analysis of the Executive Summary highlighting a clash with a paper called CachOf]
What do you think, Claude? Is this actually a novel work based on this text and these 2 papers and research reports? ... what do you think what we can actually do to make this novel by bridiging the gap or introducing any new algorithm?

**AI:**
Claude’s assessment is incredibly sharp. Using the word "caching" is dangerous here because the CachOf paper explicitly owns the "dynamic caching" space. The reframe to "Dependency-Driven Proactive Data-Plane Routing" is brilliant. 
[Explained how to pivot to Multi-Consumer Retention using eBPF BPF_MAP_TYPE_LRU_HASH to hold data exactly until the final consumer receives it]

**User:**
so now tomorrow i need to explain all this to professor under whom we are doing this so that we can agree with his terms... it would be helpful if you provide flow charts what we are fixing and everything so that i can make him understand properly

**AI:**
[Provided a step-by-step pitch for the professor, including two flowcharts demonstrating the "Status Quo" with TCP/IP overhead versus the "Novel Architecture" bypassing the network stack]

**User:**
[Uploaded DPLS and CachOf PDFs]
can you highlight the part in both the paper where it is assuming all these

**AI:**
[Pointed specifically to DPLS Page 6, Section III.C Equations 2 & 3, and CachOf Page 4 Section III.A and Page 5 Equations 3 & 4. Explained exactly how both papers treat the network as a simple mathematical constant]

**User:**
implementation plan properly for cachOF and how can we achieve it

**AI:**
[Outlined a 4-Phase implementation plan for "Dependency-Driven Data Retention": 1. Infrastructure (K3s/Cilium), 2. Control Plane (Go Scheduler), 3. Data Plane (C eBPF hooks), 4. Dependency-Driven Retention (LRU_HASH maps)]

**User:**
elaborate dependency-driven retention how to implement it actually

**AI:**
[Detailed the Kernel-Level Reference Counter, the Metadata Map (BPF_MAP_TYPE_HASH), the Payload Map (BPF_MAP_TYPE_LRU_HASH), and the atomic C logic required for kernel garbage collection]

**User:**
umm so basically we will feed DAG and the subtask that is first need to be executed to the DPLS and it will then use cilium to write down the DAG structure to the network layer telling which subtaks result is rquired for which subtask this is what you are saying?

**AI:**
You have nailed the exact architecture. 
[Clarified the 4-step sequence: Input, Calculation, System Call (The Cross-Layer Bridge), and Execution]

**User:**
okay so this cross layer bridge is the dag aware retention?

**AI:**
Not quite! The Cross-Layer Bridge is the *infrastructure* (the highway). The Dependency-Driven Retention is a specific *feature* (the delivery truck) running on that infrastructure.

**User:**
so basically our DAG will work as the retention task there is no work of eBPF maps here the bridge between two is providing the retention

**AI:**
Not quite! The DAG itself does not do the retention, and the eBPF maps are absolutely essential. The Scheduler is the blueprint (Logic), the Bridge is the messenger, and the eBPF Maps are the Vault (Physical storage).

**User:**
so basically the whole data is being held by the eBPF maps and the main channeler between two is our bridge; it will feed the eBPF maps to hold the data of which subtasks and to give them to which subtask according to the scheduler DAG

**AI:**
Exactly! You have it 100%. 
1. The DAG Scheduler (The Logic)
2. The Bridge (The Channeler)
3. The eBPF Maps (The Vault)

**User:**
na i got confused as you said eBPF maps only hold the data payloads and eBPF programs are the ones that uses these maps and feed into subtask as per the DAG and the channeler is different from this channeler tells the eBPF program to hold onto which subtasks payload for how long...

**AI:**
Yes, you have it exactly 100% right. You have perfectly separated the different moving parts of the system. 
1. DPLS (Brain)
2. Channeler/Bridge (Interface)
3. eBPF Maps (Vault)
4. eBPF Programs (Muscle)

**User:**
so how to implement this and difficulty and things to figure

**AI:**
[Detailed the engineering plan (cilium/ebpf, bpf2go), the difficulties (eBPF verifier, pointer arithmetic), and the architectural questions to answer (sk_msg vs XDP, MTU sizes, Native Offload)]

**User:**
step 1 is handled by my other teammate... So basically i am dealing with step 2 and 3 so right now we will assume that DPLS is giving us the DAG oriented output and other things that are required from the channeller as well

**AI:**
[Defined the API Contract between the teammates (DependencyRule struct) and outlined the Go Channeler logic and the C Vault/Muscle logic]

**User:**
um basically can you tell me based on the maths and other things the difficulty in implementing sk_msg vs XDP

**AI:**
[Explained why XDP is high difficulty (manual pointer arithmetic, manual checksum recalculation) compared to sk_msg (kernel handles TCP/IP parsing). Recommended sk_msg]

**User:**
but if we are using sk_msg then we never bypassed the network overhead that we were trying to do

**AI:**
You are exactly right, and this is a brilliant catch. If you use sk_msg, the packet still has to travel down Node 1's TCP/IP stack. To bypass this, we will use the TC (Traffic Control) eBPF Hook. It intercepts before iptables/TCP routing but provides helper functions for checksums.

**User:**
[Uploaded `dpls_runtime_blueprint.md`]
my main focus is whether i can actually prove what we are doing is possible in implementation

**AI:**
Yes, but the simulation currently uses `time.Sleep`. To trigger the eBPF TC hook, your friend must modify the worker to open a UDP socket and send a dummy payload. That real packet will trigger your C code.

**User:**
so my main task is to write the channeller, which will be called before every assigned task and after that i need to write the eBPF program that will handle the providing of task result to the nodes

**AI:**
You have hit the nail on the head. Task 1: The Channeler (Go, pre-programs rules). Task 2: The eBPF Program (C, intercepts packets based on the rules).

**User:**
now i want you to help me setup my ubuntu 22.04 because it will be same for all three of us i suppose so if anyone could setup his/her system can be used as benchmark

**AI:**
[Provided the complete bash setup script including LLVM toolchain, Go 1.22 + bpf2go, K3s (with flannel/kube-proxy disabled), and Cilium CNI in strict eBPF mode]

**User:**
can you create a research prompt for another chat so that i can use it to check whether what we are going to do is actually implemented anywhere in IEEE or not

**AI:**
[Provided a rigorous LLM prompt to cross-check IEEE Xplore, ACM, and arXiv for the exact intersection of DAG-aware scheduling and eBPF in-kernel retention]