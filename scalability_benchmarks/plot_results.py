import pandas as pd
import matplotlib.pyplot as plt
import sys
import os

try:
    print("Reading fanout_results.csv...")
    df_fanout = pd.read_csv('fanout_results.csv')
    plt.figure(figsize=(10, 5))
    plt.plot(df_fanout['Iteration'], df_fanout['Makespan_ms'], color='#0078D7', alpha=0.8, linewidth=1.5)
    plt.title('Fan-Out Scale Test: O(1) eBPF Broadcast (1,000 Iterations)', fontsize=14, fontweight='bold')
    plt.xlabel('Iteration Number', fontsize=12)
    plt.ylabel('Network Latency / Makespan (ms)', fontsize=12)
    plt.grid(True, linestyle='--', alpha=0.6)
    
    # Calculate average
    avg_fanout = df_fanout['Makespan_ms'].mean()
    plt.axhline(y=avg_fanout, color='orange', linestyle='--', label=f'Average: {avg_fanout:.2f} ms')
    plt.legend()
    
    # Set Y-axis to start at 0, but cap at 150% of the mean to cut off crazy outliers
    max_y = max(avg_fanout * 1.5, df_fanout['Makespan_ms'].quantile(0.99) * 1.2)
    plt.ylim(0, max_y)
    
    plt.tight_layout()
    plt.savefig('fanout_graph.png', dpi=300)
    plt.close()
    print("Generated fanout_graph.png successfully.")
    
except Exception as e:
    print(f"Error plotting fanout: {e}")

try:
    print("Reading pipeline_results.csv...")
    df_pipeline = pd.read_csv('pipeline_results.csv')
    plt.figure(figsize=(10, 5))
    plt.plot(df_pipeline['Iteration'], df_pipeline['Makespan_ms'], color='#E81123', alpha=0.8, linewidth=1.0)
    plt.title('Deep Pipeline Test: 20-Hop Depth Stability (10,000 Iterations)', fontsize=14, fontweight='bold')
    plt.xlabel('Iteration Number', fontsize=12)
    plt.ylabel('Network Latency / Makespan (ms)', fontsize=12)
    plt.grid(True, linestyle='--', alpha=0.6)
    
    # Calculate average
    avg_pipeline = df_pipeline['Makespan_ms'].mean()
    plt.axhline(y=avg_pipeline, color='orange', linestyle='--', label=f'Average: {avg_pipeline:.2f} ms')
    plt.legend()
    
    max_y_pipe = max(avg_pipeline * 1.5, df_pipeline['Makespan_ms'].quantile(0.99) * 1.2)
    plt.ylim(0, max_y_pipe)
    
    plt.tight_layout()
    plt.savefig('pipeline_graph.png', dpi=300)
    plt.close()
    print("Generated pipeline_graph.png successfully.")
    
except Exception as e:
    print(f"Error plotting pipeline: {e}")
