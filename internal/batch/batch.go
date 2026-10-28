package batch

import (
	"context"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"bserver/internal/sshx"
	"bserver/internal/store"
)

const maxOutput = 256 * 1024

type Request struct {
	ServerIDs   []string `json:"serverIds"`
	Command     string   `json:"command"`
	Concurrency int      `json:"concurrency"`
	TimeoutSec  int      `json:"timeoutSec"`
}

type Result struct {
	ServerID   string `json:"serverId"`
	Name       string `json:"name"`
	Status     string `json:"status"` // ok | error | timeout
	ExitCode   int    `json:"exitCode"`
	DurationMs int64  `json:"durationMs"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	Truncated  bool   `json:"truncated"`
}

type Summary struct {
	Total   int   `json:"total"`
	Success int   `json:"success"`
	Failed  int   `json:"failed"`
	Timeout int   `json:"timeout"`
	Elapsed int64 `json:"elapsedMs"`
}

type limitedBuf struct {
	mu        sync.Mutex
	buf       []byte
	limit     int
	truncated bool
}

// Write hanya mengimplementasikan io.Writer (tanpa ReadFrom) agar io.Copy
// selalu melewati jalur ini; embedded bytes.Buffer akan memakai ReadFrom
// yang melewati mutex dan menyebabkan output korup/kosong.
func (l *limitedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	if len(l.buf) >= l.limit {
		l.truncated = true
		return n, nil
	}
	if room := l.limit - len(l.buf); len(p) > room {
		p = p[:room]
		l.truncated = true
	}
	l.buf = append(l.buf, p...)
	return n, nil
}

func (l *limitedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.buf)
}

func (l *limitedBuf) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buf)
}

func (l *limitedBuf) isTruncated() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.truncated
}

func Run(ctx context.Context, d *sshx.Dialer, st *store.Store, req Request) ([]Result, Summary) {
	start := time.Now()
	if req.Concurrency <= 0 {
		req.Concurrency = 5
	}
	if req.Concurrency > 20 {
		req.Concurrency = 20
	}
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 30
	}
	if len(req.ServerIDs) == 0 {
		return []Result{}, Summary{}
	}

	results := make([]Result, len(req.ServerIDs))
	sem := make(chan struct{}, req.Concurrency)
	var wg sync.WaitGroup

	for i, id := range req.ServerIDs {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = runOne(ctx, d, st, id, req)
		}(i, id)
	}
	wg.Wait()

	sum := Summary{Total: len(results)}
	for _, r := range results {
		switch r.Status {
		case "ok":
			sum.Success++
		case "timeout":
			sum.Timeout++
		default:
			sum.Failed++
		}
	}
	sum.Elapsed = time.Since(start).Milliseconds()
	return results, sum
}

func runOne(ctx context.Context, d *sshx.Dialer, st *store.Store, id string, req Request) Result {
	res := Result{ServerID: id}
	t0 := time.Now()
	sv, err := st.GetServer(id)
	if err != nil {
		res.Status, res.Error = "error", "server tidak ditemukan"
		return res
	}
	res.Name = sv.Name

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutSec)*time.Second)
	defer cancel()

	client, err := d.Dial(runCtx, sv)
	if err != nil {
		res.Status, res.Error = "error", err.Error()
		res.DurationMs = time.Since(t0).Milliseconds()
		st.AddLog(id, "batch", req.Command, -1, res.DurationMs, err.Error())
		return res
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		res.Status, res.Error = "error", err.Error()
		res.DurationMs = time.Since(t0).Milliseconds()
		return res
	}
	defer sess.Close()

	buf := &limitedBuf{limit: maxOutput}
	sess.Stdout = buf
	sess.Stderr = buf

	errCh := make(chan error, 1)
	go func() { errCh <- sess.Run(req.Command) }()

	select {
	case <-runCtx.Done():
		sess.Signal(ssh.SIGKILL)
		sess.Close()
		res.Status = "timeout"
		res.Error = "melewati batas waktu " + time.Duration(req.TimeoutSec*int(time.Second)).String()
		res.Output = buf.String()
	case err = <-errCh:
		if err == nil {
			res.Status = "ok"
		} else if ee, ok := err.(*ssh.ExitError); ok {
			res.ExitCode = ee.ExitStatus()
			res.Status = "error"
			if res.ExitCode == 0 {
				res.ExitCode = 1
			}
		} else {
			res.Status, res.Error = "error", err.Error()
		}
	}
	res.DurationMs = time.Since(t0).Milliseconds()
	res.Truncated = buf.isTruncated()
	res.Output = buf.String()
	st.AddLog(id, "batch", req.Command, res.ExitCode, res.DurationMs, trim(res.Output, 2000))
	return res
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// SortByID dipakai untuk membuat urutan deterministik saat uji.
func SortByID(rs []Result) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
}
