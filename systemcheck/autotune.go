package systemcheck

import (
    "bufio"
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "time"

    "golang.org/x/sys/unix"
)

// ApplyAutoTuning attempts to automatically optimize kernel and process settings
// based on current system resources. It is safe to call without root privileges;
// in that case only process-level settings (rlimit, GOMAXPROCS) are applied.
func ApplyAutoTuning() {
    // 1) Auto GOMAXPROCS if not set explicitly
    if os.Getenv("GOMAXPROCS") == "" {
        runtime.GOMAXPROCS(runtime.NumCPU())
    }

    // 2) Process rlimits (no root required)
    raiseNoFile(1048576)     // try raise to 1M
    raiseNProc(262144)       // try raise to 262k

    // 3) Kernel sysctls (root required, best-effort)
    if os.Geteuid() == 0 {
        totalMem := memTotalBytes()
        // Target sizes based on available memory
        // rmem/wmem target between 64MB and 256MB, capped by totalMem/32
        targetBuf := clamp64(64<<20, min64(256<<20, totalMem/32))
        targetMin := clamp64(4<<20, min64(16<<20, targetBuf/16))

        _ = writeSysctlInt("net/core/rmem_max", int64(targetBuf))
        _ = writeSysctlInt("net/core/wmem_max", int64(targetBuf))
        _ = writeSysctlInt("net/core/rmem_default", int64(targetBuf))
        _ = writeSysctlInt("net/core/wmem_default", int64(targetBuf))
        _ = writeSysctlInt("net/ipv4/udp_rmem_min", int64(targetMin))
        _ = writeSysctlInt("net/ipv4/udp_wmem_min", int64(targetMin))
        _ = writeSysctlInt("net/core/netdev_max_backlog", 250000)
        _ = writeSysctlInt("net/core/somaxconn", 65535)
    }
}

func raiseNoFile(want uint64) {
    var r unix.Rlimit
    if unix.Getrlimit(unix.RLIMIT_NOFILE, &r) != nil { return }
    if r.Cur >= want { return }
    newCur := want
    if r.Max > 0 && newCur > r.Max { newCur = r.Max }
    r.Cur = newCur
    _ = unix.Setrlimit(unix.RLIMIT_NOFILE, &r)
}

func raiseNProc(want uint64) {
    var r unix.Rlimit
    if unix.Getrlimit(unix.RLIMIT_NPROC, &r) != nil { return }
    if r.Cur >= want { return }
    newCur := want
    if r.Max > 0 && newCur > r.Max { newCur = r.Max }
    r.Cur = newCur
    _ = unix.Setrlimit(unix.RLIMIT_NPROC, &r)
}

func memTotalBytes() int64 {
    f, err := os.Open("/proc/meminfo")
    if err != nil { return 8 << 30 } // fallback 8GB
    defer f.Close()
    s := bufio.NewScanner(f)
    for s.Scan() {
        line := s.Text()
        if strings.HasPrefix(line, "MemTotal:") {
            fields := strings.Fields(line)
            if len(fields) >= 2 {
                // value in kB
                if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
                    return kb * 1024
                }
            }
        }
    }
    return 8 << 30
}

func writeSysctlInt(key string, value int64) error {
    path := filepath.Join("/proc/sys", key)
    // Convert to string without trailing newline as /proc expects
    data := []byte(strconv.FormatInt(value, 10))
    // Retry a couple of times in case sysctl maps are being updated concurrently
    for i := 0; i < 2; i++ {
        if err := os.WriteFile(path, data, 0o644); err == nil {
            return nil
        } else {
            time.Sleep(50 * time.Millisecond)
        }
    }
    return fmt.Errorf("write sysctl %s failed", key)
}

func min64(a, b int64) int64 { if a < b { return a }; return b }
func clamp64(min, val int64) int64 { if val < min { return min }; return val }


