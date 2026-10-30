#!/usr/bin/env python3
"""Generate a bounded, loopback-only configuration from the release defaults."""
import os
from pathlib import Path

root = Path(os.environ.get("LAB_ROOT", "/home/homelab/.local/share/lewbox-agentenv-lab"))
text = (root / "server/default.toml").read_text()
changes = {
    'home_path = "/var/lib/aenv"': f'home_path = "{root}/state"',
    'capacity_gb = 100': 'capacity_gb = 6',
    'max_size_gb = 100': 'max_size_gb = 4',
    'mem_size_mib = 1024': 'mem_size_mib = 512',
    'vcpu_count = 2': 'vcpu_count = 1',
    'default_sandbox_timeout_secs = 15': 'default_sandbox_timeout_secs = 600',
    'daemon_metrics_listen_addr = "0.0.0.0:9103"': 'daemon_metrics_listen_addr = "127.0.0.1:19103"',
    'low_watermark = 2': 'low_watermark = 1',
    'high_watermark = 64': 'high_watermark = 2',
    'max_concurrent_builds = 4': 'max_concurrent_builds = 1',
    'builder_cpu_count = 16': 'builder_cpu_count = 2',
    'builder_memory_mb = 32768': 'builder_memory_mb = 2048',
    'cache_size_mb = 65536': 'cache_size_mb = 8192',
    'node_id = "node-a"': 'node_id = "lewbox-lab"',
    'service_instance_id = "service-instance-a"': 'service_instance_id = "lewbox-agentenv-lab"',
}
for old, new in changes.items():
    if old not in text:
        raise SystemExit(f"Release default missing expected setting: {old}")
    text = text.replace(old, new, 1)
(root / "config.toml").write_text(text)
for directory in ["state", "run", "logs"]:
    (root / directory).mkdir(exist_ok=True)
print(f"Configured {root / 'config.toml'}")
