package metrics

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"bserver/internal/sshx"
	"bserver/internal/store"
)

type Snapshot struct {
	ServerID  string  `json:"serverId"`
	Name      string  `json:"name"`
	OK        bool    `json:"ok"`
	Error     string  `json:"error,omitempty"`
	Hostname  string  `json:"hostname,omitempty"`
	Kernel    string  `json:"kernel,omitempty"`
	CPUPct    float64 `json:"cpuPct"`
	MemUsed   uint64  `json:"memUsedKb"`
	MemTotal  uint64  `json:"memTotalKb"`
	DiskUsed  uint64  `json:"diskUsedKb"`
	DiskTotal uint64  `json:"diskTotalKb"`
	UptimeSec int64   `json:"uptimeSec"`
	Load1     float64 `json:"load1"`
	LatencyMs int64   `json:"latencyMs"`
}

const script = `echo "@@stat1"; head -1 /proc/stat
echo "@@stat2"; sleep 0.3; head -1 /proc/stat
echo "@@mem"; grep -E "^(MemTotal|MemAvailable|MemFree|Buffers|Cached):" /proc/meminfo
echo "@@disk"; df -Pk / | tail -1
echo "@@up"; cat /proc/uptime
echo "@@load"; cat /proc/loadavg
echo "@@host"; hostname; uname -r`

type Collector struct {
	Dialer *sshx.Dialer
	Store  *store.Store
}

func (c *Collector) Snapshot(ctx context.Context, ids []string) []Snapshot {
	out := make([]Snapshot, len(ids))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = c.one(ctx, id)
		}(i, id)
	}
	wg.Wait()
	return out
}

func (c *Collector) one(ctx context.Context, id string) Snapshot {
	s := Snapshot{ServerID: id}
	sv, err := c.Store.GetServer(id)
	if err != nil {
		s.Error = "server tidak ditemukan"
		return s
	}
	s.Name = sv.Name
	start := time.Now()
	client, err := c.Dialer.Dial(ctx, sv)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	defer client.Close()
	s.LatencyMs = time.Since(start).Milliseconds()

	sess, err := client.NewSession()
	if err != nil {
		s.Error = err.Error()
		return s
	}
	defer sess.Close()
	out, err := sess.Output("sh -c '" + strings.ReplaceAll(script, "'", "'\\''") + "'")
	if err != nil {
		s.Error = "gagal membaca metrik: " + err.Error()
		return s
	}
	parse(string(out), &s)
	s.OK = s.MemTotal > 0
	if !s.OK {
		s.Error = "format /proc tidak dikenali"
	}
	return s
}

func parse(out string, s *Snapshot) {
	var stat1, stat2 []string
	var memAvail uint64
	section := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "@@") {
			section = strings.TrimSpace(line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch section {
		case "@@stat1":
			stat1 = strings.Fields(line)
		case "@@stat2":
			stat2 = strings.Fields(line)
		case "@@mem":
			f := strings.Fields(line)
			if len(f) >= 2 {
				v, _ := strconv.ParseUint(f[1], 10, 64)
				switch f[0] {
				case "MemTotal:":
					s.MemTotal = v
				case "MemAvailable:":
					memAvail = v
				}
			}
		case "@@disk":
			f := strings.Fields(line)
			if len(f) >= 3 {
				total, _ := strconv.ParseUint(f[1], 10, 64)
				used, _ := strconv.ParseUint(f[2], 10, 64)
				s.DiskTotal, s.DiskUsed = total, used
			}
		case "@@up":
			f := strings.Fields(line)
			if len(f) > 0 {
				u, _ := strconv.ParseFloat(f[0], 64)
				s.UptimeSec = int64(u)
			}
		case "@@load":
			f := strings.Fields(line)
			if len(f) > 0 {
				s.Load1, _ = strconv.ParseFloat(f[0], 64)
			}
		case "@@host":
			if s.Hostname == "" {
				s.Hostname = strings.TrimSpace(line)
			} else {
				s.Kernel = strings.TrimSpace(line)
			}
		}
	}
	if s.MemTotal > 0 && memAvail <= s.MemTotal {
		s.MemUsed = s.MemTotal - memAvail
	}
	if len(stat1) > 3 && len(stat2) > 3 {
		var idle1, idle2, tot1, tot2 float64
		for i := 1; i < len(stat1); i++ {
			v, _ := strconv.ParseFloat(stat1[i], 64)
			tot1 += v
			if i == 4 || i == 5 {
				idle1 += v
			}
		}
		for i := 1; i < len(stat2); i++ {
			v, _ := strconv.ParseFloat(stat2[i], 64)
			tot2 += v
			if i == 4 || i == 5 {
				idle2 += v
			}
		}
		if tot2 > tot1 && idle2 >= idle1 {
			s.CPUPct = (1 - (idle2-idle1)/(tot2-tot1)) * 100
			if s.CPUPct < 0 {
				s.CPUPct = 0
			}
		}
	}
}
