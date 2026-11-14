# sshud probe. POSIX sh only (busybox compatible), read-only.
# Runs as an authorized_keys forced command; the level (overview or detail)
# comes from $SSH_ORIGINAL_COMMAND.
# Every section starts with "@@<name>"; a section that fails prints "@@err".
# Keep it joinable into one line: no heredocs, comments on their own lines.
L="$SSH_ORIGINAL_COMMAND"
s() { echo "@@$1"; }
s stat1;    cat /proc/stat 2>/dev/null || echo @@err
# network counters and clock before the pause give a rate in the first frame
s netdev0;  cat /proc/net/dev 2>/dev/null || echo @@err
s uptime0;  cat /proc/uptime 2>/dev/null || echo @@err
# busybox builds without fractional sleep fail fast; fall back to a whole second
sleep 0.5 2>/dev/null || sleep 1
s stat2;    cat /proc/stat 2>/dev/null || echo @@err
s meminfo;  cat /proc/meminfo 2>/dev/null || echo @@err
s loadavg;  cat /proc/loadavg 2>/dev/null || echo @@err
s uptime;   cat /proc/uptime 2>/dev/null || echo @@err
s netdev;   cat /proc/net/dev 2>/dev/null || echo @@err
s df;       df -P -k 2>/dev/null || echo @@err
s uname;    uname -srm 2>/dev/null || echo @@err
s hostname; cat /proc/sys/kernel/hostname 2>/dev/null || echo @@err
# Synology DSM has no os-release; its VERSION file uses the same key="value" form
s osrelease; cat /etc/os-release 2>/dev/null || cat /etc.defaults/VERSION 2>/dev/null || echo @@err
[ "$L" = detail ] || exit 0
s conf;     echo "clk_tck $(getconf CLK_TCK 2>/dev/null)"; echo "pagesize $(getconf PAGESIZE 2>/dev/null)"
s snmp;     cat /proc/net/snmp 2>/dev/null || echo @@err
s diskstats; cat /proc/diskstats 2>/dev/null || echo @@err
# per process: pid utime stime rss comm (comm may contain spaces and parentheses)
s procs;    cat /proc/[0-9]*/stat 2>/dev/null | awk '{l=$0; p=index(l," ("); e=0; for(i=length(l);i>p;i--) if(substr(l,i,1)==")"){e=i;break}; split(substr(l,e+2),f," "); print substr(l,1,p-1), f[12], f[13], f[22], substr(l,p+2,e-p-2)}' || echo @@err
s users;    who 2>/dev/null || echo @@err
s docker;   docker ps -a --no-trunc --format '{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.State}}\t{{.Status}}' 2>/dev/null || echo @@err
# per-container usage from cgroups, readable without root: "v2 <dir> <cpu usec> <memory> <inactive file>"
# (systemd or cgroupfs driver) or "v1 <id> <cpu nsec> <memory> <inactive file>"
s cgroup;   for d in /sys/fs/cgroup/system.slice/docker-*.scope /sys/fs/cgroup/docker/*; do [ -r "$d/cpu.stat" ] && echo "v2 ${d##*/} $(awk '/^usage_usec/{print $2}' "$d/cpu.stat") $(cat "$d/memory.current") $(awk '/^inactive_file /{print $2}' "$d/memory.stat")"; done 2>/dev/null
for d in /sys/fs/cgroup/memory/docker/*; do [ -r "$d/memory.usage_in_bytes" ] && echo "v1 ${d##*/} $(cat "/sys/fs/cgroup/cpuacct/docker/${d##*/}/cpuacct.usage") $(cat "$d/memory.usage_in_bytes") $(awk '/^total_inactive_file /{print $2}' "$d/memory.stat")"; done 2>/dev/null
s thermal;  for z in /sys/class/thermal/thermal_zone*; do [ -r "$z/temp" ] && echo "$(cat "$z/type" 2>/dev/null) $(cat "$z/temp")"; done 2>/dev/null
s addr;     ip -o addr show 2>/dev/null || echo @@err
s updates;  cat /var/lib/update-notifier/updates-available 2>/dev/null || echo @@err
s reboot;   [ -f /var/run/reboot-required ] && echo yes || echo no
