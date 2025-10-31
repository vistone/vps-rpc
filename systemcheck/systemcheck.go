package systemcheck

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Result captures a single check outcome
type Result struct {
	Name        string
	Current     string
	Recommended string
	OK          bool
	Note        string
}

// RunPreflightChecks inspects kernel/sysctl and process limits relevant to QUIC performance.
// It only reads and reports; no system mutation is attempted.
func RunPreflightChecks() []Result {
	var results []Result

	// sysctl targets and recommended values
	checks := []struct{
		key         string
		recommend   int64
		displayUnit string
	}{
		{"net/core/rmem_max", 64 << 20, "bytes"},
		{"net/core/wmem_max", 64 << 20, "bytes"},
		{"net/core/rmem_default", 64 << 20, "bytes"},
		{"net/core/wmem_default", 64 << 20, "bytes"},
		{"net/ipv4/udp_rmem_min", 4 << 20, "bytes"},
		{"net/ipv4/udp_wmem_min", 4 << 20, "bytes"},
		{"net/core/netdev_max_backlog", 250000, "entries"},
		{"net/core/somaxconn", 65535, "entries"},
	}

	for _, c := range checks {
		val, err := readSysctlInt(c.key)
		ok := err == nil && val >= c.recommend
		curStr := "unknown"
		if err == nil { curStr = fmt.Sprintf("%d %s", val, c.displayUnit) }
		recStr := fmt.Sprintf("%d %s", c.recommend, c.displayUnit)
		note := ""
		if err != nil { note = err.Error() }
		results = append(results, Result{
			Name:        "sysctl:" + strings.ReplaceAll(c.key, "/", "."),
			Current:     curStr,
			Recommended: recStr,
			OK:          ok,
			Note:        note,
		})
	}

	// Process limits
	var rlim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &rlim); err == nil {
		results = append(results, Result{
			Name:        "rlimit.nofile",
			Current:     fmt.Sprintf("cur=%d max=%d", rlim.Cur, rlim.Max),
			Recommended: "cur>=1048576",
			OK:          rlim.Cur >= 1048576,
		})
	} else {
		results = append(results, Result{Name: "rlimit.nofile", Current: "unknown", Recommended: "cur>=1048576", OK: false, Note: err.Error()})
	}
	if err := unix.Getrlimit(unix.RLIMIT_NPROC, &rlim); err == nil {
		results = append(results, Result{
			Name:        "rlimit.nproc",
			Current:     fmt.Sprintf("cur=%d max=%d", rlim.Cur, rlim.Max),
			Recommended: "cur>=262144",
			OK:          rlim.Cur >= 262144,
		})
	} else {
		results = append(results, Result{Name: "rlimit.nproc", Current: "unknown", Recommended: "cur>=262144", OK: false, Note: err.Error()})
	}

	// Go runtime threads
	gomax := runtime.GOMAXPROCS(0)
	results = append(results, Result{
		Name:        "runtime.GOMAXPROCS",
		Current:     strconv.Itoa(gomax),
		Recommended: "= NumCPU (auto)",
		OK:          gomax == runtime.NumCPU(),
	})

	// Emit a concise summary to log for visibility
	log.Println("[preflight] System checks (read-only):")
	for _, r := range results {
		status := "OK"
		if !r.OK { status = "WARN" }
		if r.Note != "" {
			log.Printf("[preflight] %-6s %-24s current=%s recommended=%s note=%s", status, r.Name, r.Current, r.Recommended, r.Note)
		} else {
			log.Printf("[preflight] %-6s %-24s current=%s recommended=%s", status, r.Name, r.Current, r.Recommended)
		}
	}
	return results
}

// readSysctlInt reads /proc/sys/<key> where key like net/core/rmem_max
func readSysctlInt(key string) (int64, error) {
	path := filepath.Join("/proc/sys", key)
	f, err := os.Open(path)
	if err != nil { return 0, err }
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() { return 0, fmt.Errorf("empty sysctl: %s", key) }
	text := strings.TrimSpace(s.Text())
	// Some values may contain whitespace; take first token
	fields := strings.Fields(text)
	if len(fields) == 0 { return 0, fmt.Errorf("invalid sysctl: %s", key) }
	return strconv.ParseInt(fields[0], 10, 64)
}


