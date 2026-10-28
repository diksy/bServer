package metrics

import (
	"context"
	"fmt"
	"log"
	"time"

	"bserver/internal/store"
	"bserver/internal/vault"
)

// Sampler mengambil snapshot metrik semua server secara berkala dan
// menyimpannya ke database untuk grafik historis.
//
// Sampling dilewati saat vault terkunci, karena data kredensial belum bisa
// didekripsi (ListServers/Snapshot akan gagal).
type Sampler struct {
	Store     *store.Store
	Collector *Collector
	Vault     *vault.Vault
	Interval  time.Duration
}

// sampleTimeout membatasi durasi satu putaran sampling agar tidak menumpuk
// bila salah satu server lambat.
const sampleTimeout = 45 * time.Second

func (s *Sampler) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return 60 * time.Second
}

// Run menjalankan loop sampling sampai ctx dibatalkan.
func (s *Sampler) Run(ctx context.Context) {
	if s.Store == nil || s.Collector == nil {
		log.Printf("sampler metrik: nonaktif (store/collector kosong)")
		return
	}
	iv := s.interval()
	log.Printf("sampler metrik: aktif tiap %s", iv)
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("sampler metrik: berhenti")
			return
		case <-t.C:
			s.sample(ctx)
		}
	}
}

// sample melakukan satu putaran pengambilan metrik. Error tidak fatal.
func (s *Sampler) sample(parent context.Context) {
	if s.Vault != nil && !s.Vault.Unlocked() {
		return // vault terkunci: tidak ada yang bisa diambil
	}
	servers, err := s.Store.ListServers("")
	if err != nil {
		log.Printf("sampler metrik: daftar server gagal: %v", err)
		return
	}
	if len(servers) == 0 {
		return
	}
	ids := make([]string, 0, len(servers))
	for _, sv := range servers {
		ids = append(ids, sv.ID)
	}

	thresholds, err := s.Store.GetAlertThresholds()
	if err != nil {
		log.Printf("sampler metrik: baca ambang alert gagal: %v", err)
	}

	ctx, cancel := context.WithTimeout(parent, sampleTimeout)
	defer cancel()

	saved, failed := 0, 0
	for _, snap := range s.Collector.Snapshot(ctx, ids) {
		if !snap.OK {
			failed++
			continue
		}
		p := store.MetricPoint{
			CPU:         snap.CPUPct,
			MemUsedKb:   int64(snap.MemUsed),
			MemTotalKb:  int64(snap.MemTotal),
			DiskUsedKb:  int64(snap.DiskUsed),
			DiskTotalKb: int64(snap.DiskTotal),
			Load1:       snap.Load1,
		}
		if err := s.Store.InsertMetricPoint(snap.ServerID, p); err != nil {
			log.Printf("sampler metrik: simpan %s gagal: %v", snap.ServerID, err)
			continue
		}
		saved++
		checkThresholds(s.Store, snap, thresholds)
	}
	if saved > 0 || failed > 0 {
		log.Printf("sampler metrik: %d titik tersimpan, %d server gagal", saved, failed)
	}
}

// checkThresholds membandingkan satu snapshot dengan ambang dan mencatat
// alert lokal bila terlampaui. Error bersifat non-fatal (hanya dicatat).
func checkThresholds(st *store.Store, snap Snapshot, t store.AlertThresholds) {
	checks := []struct {
		kind      string
		label     string
		value     float64
		threshold float64
	}{
		{"cpu", "CPU", snap.CPUPct, t.CPUPct},
		{"mem", "RAM", memPct(snap), t.MemPct},
		{"disk", "Disk", diskPct(snap), t.DiskPct},
	}
	for _, c := range checks {
		if c.threshold <= 0 || c.value <= c.threshold {
			continue
		}
		msg := fmt.Sprintf("%s %.1f%% melebihi ambang %.1f%%", c.label, c.value, c.threshold)
		if err := st.InsertAlert(snap.ServerID, c.kind, c.value, c.threshold, msg); err != nil {
			log.Printf("sampler metrik: simpan alert %s/%s gagal: %v", snap.ServerID, c.kind, err)
		}
	}
}

func memPct(snap Snapshot) float64 {
	if snap.MemTotal <= 0 {
		return 0
	}
	return float64(snap.MemUsed) / float64(snap.MemTotal) * 100
}

func diskPct(snap Snapshot) float64 {
	if snap.DiskTotal <= 0 {
		return 0
	}
	return float64(snap.DiskUsed) / float64(snap.DiskTotal) * 100
}
