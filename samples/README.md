# Probe samples

Each directory holds raw output of `internal/probe/probe.sh` for one system.
Real captures are sanitized: hostnames, IPs, user names, container names and
private service names are replaced with example values.

| Directory | Source |
|---|---|
| `ubuntu-24.04-x86-vm` | real (cloud VM, no Docker) |
| `ubuntu-24.04-x86-docker` | real (home server with Docker, CIFS mounts) |
| `alpine-3.20-busybox` | hand-written: busybox `df`/`ip`, no `getconf`, no `who`, no os updates |
| `raspbian-12-armv7` | hand-written: 4 cores, SD card (`mmcblk0`), `cpu-thermal`, `/proc/stat` with only 8 columns (no guest) |
| `oraclelinux-9-x86` | hand-written: LVM root on `/dev/mapper`, XFS, no update-notifier, no Docker |
| `synology-dsm7` | hand-written: `/volume1` on md RAID, Docker not readable without root |
