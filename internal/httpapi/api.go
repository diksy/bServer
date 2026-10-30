package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"bserver/internal/batch"
	"bserver/internal/config"
	"bserver/internal/metrics"
	"bserver/internal/sftpx"
	"bserver/internal/sshx"
	"bserver/internal/store"
	"bserver/internal/svcs"
	"bserver/internal/term"
	"bserver/internal/vault"
)

type API struct {
	Cfg     *config.Config
	Vault   *vault.Vault
	Store   *store.Store
	Dialer  *sshx.Dialer
	Sftp    *sftpx.Lister
	Svcs    *svcs.Lister
	Metrics *metrics.Collector
	Assets  fs.FS
	Start   time.Time
	Port    int

	hub *wsHub

	quickMu sync.Mutex
	quick   map[string]quickEntry
}

// quickEntry menyimpan kredensial quick connect di memory saja (single-use,
// berumur pendek). Tidak pernah ditulis ke database.
type quickEntry struct {
	in  store.ServerInput
	exp time.Time
}

func (a *API) hubInit() *wsHub {
	if a.hub == nil {
		a.hub = newWSHub()
	}
	return a.hub
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/status", a.status)
	mux.HandleFunc("POST /api/vault/create", a.vaultCreate)
	mux.HandleFunc("POST /api/vault/unlock", a.vaultUnlock)
	mux.HandleFunc("POST /api/vault/lock", a.vaultLock)

	mux.HandleFunc("GET /api/servers", a.listServers)
	mux.HandleFunc("POST /api/servers", a.createServer)
	mux.HandleFunc("GET /api/servers/{id}", a.getServer)
	mux.HandleFunc("PATCH /api/servers/{id}", a.updateServer)
	mux.HandleFunc("DELETE /api/servers/{id}", a.deleteServer)
	mux.HandleFunc("POST /api/servers/{id}/test", a.testServer)
	mux.HandleFunc("POST /api/servers/{id}/knownhost/reset", a.resetHostKey)
	mux.HandleFunc("POST /api/test", a.testAdhoc)
	mux.HandleFunc("POST /api/quick", a.quickCreate)

	mux.HandleFunc("GET /api/groups", a.listGroups)
	mux.HandleFunc("POST /api/groups", a.createGroup)
	mux.HandleFunc("PATCH /api/groups/{id}", a.updateGroup)
	mux.HandleFunc("DELETE /api/groups/{id}", a.deleteGroup)

	mux.HandleFunc("GET /api/snippets", a.listSnippets)
	mux.HandleFunc("POST /api/snippets", a.createSnippet)
	mux.HandleFunc("PATCH /api/snippets/{id}", a.updateSnippet)
	mux.HandleFunc("DELETE /api/snippets/{id}", a.deleteSnippet)

	mux.HandleFunc("POST /api/import/sshconfig", a.importSSHConfig)
	mux.HandleFunc("POST /api/import", a.importJSON)
	mux.HandleFunc("GET /api/export", a.exportJSON)

	mux.HandleFunc("POST /api/exec", a.execBatch)
	mux.HandleFunc("GET /api/logs", a.logs)

	mux.HandleFunc("GET /api/sftp/list", a.sftpList)
	mux.HandleFunc("POST /api/sftp/mkdir", a.sftpMkdir)
	mux.HandleFunc("POST /api/sftp/upload", a.sftpUpload)
	mux.HandleFunc("GET /api/sftp/download", a.sftpDownload)
	mux.HandleFunc("GET /api/sftp/read", a.sftpRead)
	mux.HandleFunc("POST /api/sftp/write", a.sftpWrite)

	mux.HandleFunc("GET /api/metrics", a.getMetrics)
	mux.HandleFunc("GET /api/metrics/history", a.metricHistory)

	mux.HandleFunc("GET /api/services", a.services)

	mux.HandleFunc("GET /api/alerts", a.listAlerts)
	mux.HandleFunc("GET /api/alerts/unacked", a.alertsUnacked)
	mux.HandleFunc("POST /api/alerts/ack", a.ackAlerts)

	mux.HandleFunc("GET /api/settings", a.getSettings)
	mux.HandleFunc("PUT /api/settings", a.putSettings)

	mux.HandleFunc("GET /ws/term", a.termWS)
	mux.HandleFunc("GET /ws/quickterm", a.quickTermWS)

	mux.HandleFunc("/", a.spa)

	return a.withToken(mux)
}

func (a *API) withToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/") && p != "/ws/term" && p != "/ws/quickterm" {
			next.ServeHTTP(w, r)
			return
		}
		// Cek Host/Origin: hanya UI lokal yang boleh memanggil API/WS.
		if !a.originAllowed(r) {
			writeErr(w, http.StatusForbidden, "origin/host tidak diizinkan")
			return
		}
		tok := r.Header.Get("X-Token")
		if tok == "" && (p == "/ws/term" || p == "/ws/quickterm") {
			// WebSocket browser tidak bisa mengirim header: token lewat query
			// hanya diizinkan untuk endpoint WS.
			tok = r.URL.Query().Get("t")
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(a.Cfg.Token)) != 1 {
			writeErr(w, http.StatusUnauthorized, "token sesi tidak valid")
			return
		}
		a.Vault.Touch()
		if !a.Vault.Unlocked() {
			// auto-lock: putuskan semua terminal yang masih terbuka
			a.hubInit().closeAll("vault terkunci")
		}
		if needsVault(p) && !a.Vault.Unlocked() {
			writeErr(w, http.StatusLocked, "vault terkunci")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed memastikan Host adalah loopback pada port aplikasi ini.
func (a *API) hostAllowed(hostport string) bool {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return false
	}
	if a.Port != 0 && port != strconv.Itoa(a.Port) {
		return false
	}
	switch strings.Trim(host, "[]") {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// originAllowed memvalidasi Host dan (bila ada) Origin. Origin kosong berarti
// klien non-browser (curl/CLI) dan diizinkan selama Host loopback.
func (a *API) originAllowed(r *http.Request) bool {
	if !a.hostAllowed(r.Host) {
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return a.hostAllowed(u.Host)
}

func needsVault(p string) bool {
	if strings.HasPrefix(p, "/api/vault") || p == "/api/status" || p == "/api/settings" {
		return false
	}
	return true
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	theme := a.Store.Setting("theme", "light")
	al, _ := strconv.Atoi(a.Store.Setting("autoLockMin", "10"))
	writeJSON(w, 200, map[string]any{
		"vaultExists":  a.Vault.Exists(),
		"unlocked":     a.Vault.Unlocked(),
		"autoLockMin":  al,
		"theme":        theme,
		"version":      "0.1.1",
		"uptimeSec":    int64(time.Since(a.Start).Seconds()),
		"dataDir":      a.Cfg.DataDir,
		"sshAgentSock": os.Getenv("SSH_AUTH_SOCK") != "",
	})
}

func (a *API) vaultCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Passphrase string `json:"passphrase"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Vault.Create(in.Passphrase); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) vaultUnlock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Passphrase string `json:"passphrase"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Vault.Unlock(in.Passphrase); err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) vaultLock(w http.ResponseWriter, r *http.Request) {
	// Tutup semua terminal aktif agar tidak ada sesi yang tetap hidup
	// setelah vault terkunci.
	a.hubInit().closeAll("vault terkunci")
	a.Vault.Lock()
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- servers ----------

func (a *API) listServers(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ListServers(r.URL.Query().Get("q"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) getServer(w http.ResponseWriter, r *http.Request) {
	sv, err := a.Store.GetServer(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, sv)
}

func (a *API) createServer(w http.ResponseWriter, r *http.Request) {
	var in store.ServerInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	sv, err := a.Store.CreateServer(in)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, sv)
}

func (a *API) updateServer(w http.ResponseWriter, r *http.Request) {
	var in store.ServerInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	sv, err := a.Store.UpdateServer(r.PathValue("id"), in)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, sv)
}

func (a *API) deleteServer(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteServer(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) testServer(w http.ResponseWriter, r *http.Request) {
	sv, err := a.Store.GetServer(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	t0 := time.Now()
	conn, err := a.Dialer.Dial(ctx, sv)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error(), "latencyMs": time.Since(t0).Milliseconds()})
		return
	}
	lat := time.Since(t0).Milliseconds()
	out, err := conn.Output("echo bserver-ok")
	conn.Close()
	ok := err == nil && strings.Contains(string(out), "bserver-ok")
	a.Store.SetServerStatus(sv.ID, int(lat), ok)
	sv, _ = a.Store.GetServer(sv.ID)
	resp := map[string]any{"ok": ok, "latencyMs": lat}
	if sv != nil {
		resp["fingerprint"], resp["hostKeyAlgo"] = sv.HostKeyFp, sv.HostKeyAlgo
	}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, 200, resp)
}

// testAdhoc menguji koneksi memakai kredensial dari form, tanpa menyimpan server.
func (a *API) testAdhoc(w http.ResponseWriter, r *http.Request) {
	var in store.ServerInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(in.Host) == "" || strings.TrimSpace(in.Username) == "" || in.AuthType == "" {
		writeErr(w, 400, "host, username, dan authType wajib diisi")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	t0 := time.Now()
	conn, err := a.Dialer.DialInput(ctx, in)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error(), "latencyMs": time.Since(t0).Milliseconds()})
		return
	}
	defer conn.Close()
	lat := time.Since(t0).Milliseconds()
	out, err := conn.Output("echo bserver-ok")
	ok := err == nil && strings.Contains(string(out), "bserver-ok")
	resp := map[string]any{
		"ok":          ok,
		"latencyMs":   lat,
		"fingerprint": conn.Fingerprint,
		"hostKeyAlgo": conn.HostKeyAlgo,
	}
	if err != nil {
		resp["error"] = err.Error()
	} else if !ok {
		resp["error"] = "perintah uji tidak mengembalikan bserver-ok"
	}
	writeJSON(w, 200, resp)
}

func (a *API) resetHostKey(w http.ResponseWriter, r *http.Request) {
	sv, err := a.Store.GetServer(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	if err := a.Store.ForgetHostKey(sv.Host, sv.Port); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- groups ----------

func (a *API) listGroups(w http.ResponseWriter, r *http.Request) {
	g, err := a.Store.ListGroups()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, g)
}

func (a *API) createGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	g, err := a.Store.CreateGroup(in.Name, in.Color)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, g)
}

func (a *API) updateGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.UpdateGroup(r.PathValue("id"), in.Name, in.Color); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteGroup(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- snippets ----------

func (a *API) listSnippets(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ListSnippets()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) createSnippet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Body string `json:"body"`
		Tags string `json:"tags"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	sn, err := a.Store.CreateSnippet(in.Name, in.Body, in.Tags)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, sn)
}

func (a *API) updateSnippet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Body string `json:"body"`
		Tags string `json:"tags"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.UpdateSnippet(r.PathValue("id"), in.Name, in.Body, in.Tags); err != nil {
		writeErr(w, snippetErrCode(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) deleteSnippet(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteSnippet(r.PathValue("id")); err != nil {
		writeErr(w, snippetErrCode(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// snippetErrCode membedakan data tidak ditemukan (404) dari input tidak valid (400).
func snippetErrCode(err error) int {
	if errors.Is(err, store.ErrNotFound) {
		return 404
	}
	return 400
}

// ---------- import / export ----------

func (a *API) exportJSON(w http.ResponseWriter, r *http.Request) {
	servers, err := a.Store.ListServers("")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	groups, _ := a.Store.ListGroups()
	type expServer struct {
		Name      string `json:"name"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		Username  string `json:"username"`
		AuthType  string `json:"authType"`
		GroupName string `json:"groupName,omitempty"`
		JumpName  string `json:"jumpName,omitempty"`
		Tags      string `json:"tags"`
		Color     string `json:"color"`
		Notes     string `json:"notes"`
		Favorite  bool   `json:"favorite"`
		KeyPath   string `json:"keyPath,omitempty"`
	}
	out := struct {
		App     string        `json:"app"`
		Version int           `json:"version"`
		Groups  []store.Group `json:"groups"`
		Servers []expServer   `json:"servers"`
	}{App: "bServer", Version: 1, Groups: groups, Servers: []expServer{}}
	for _, s := range servers {
		e := expServer{Name: s.Name, Host: s.Host, Port: s.Port, Username: s.Username, AuthType: s.AuthType,
			Tags: s.Tags, Color: s.Color, Notes: s.Notes, Favorite: s.Favorite, KeyPath: s.KeyPath}
		if s.GroupName != nil {
			e.GroupName = *s.GroupName
		}
		if s.JumpName != nil {
			e.JumpName = *s.JumpName
		}
		out.Servers = append(out.Servers, e)
	}
	w.Header().Set("Content-Disposition", `attachment; filename="bserver-inventaris.json"`)
	writeJSON(w, 200, out)
}

// importServer menambahkan referensi berbasis nama (groupName/jumpName) supaya
// hasil export bisa diimpor kembali tanpa kehilangan grup & jump host.
type importServer struct {
	store.ServerInput
	GroupName string `json:"groupName"`
	JumpName  string `json:"jumpName"`
}

func (a *API) importJSON(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Groups  []store.Group  `json:"groups"`
		Servers []importServer `json:"servers"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// dukung juga array langsung
	var arr []importServer
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
		in.Servers = arr
	} else if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, 400, "JSON tidak valid: "+err.Error())
		return
	}

	// Peta nama -> id untuk resolusi grup & jump host.
	groupID := map[string]string{}
	if groups, err := a.Store.ListGroups(); err == nil {
		for _, g := range groups {
			groupID[strings.ToLower(g.Name)] = g.ID
		}
	}
	serverID := map[string]string{}
	if servers, err := a.Store.ListServers(""); err == nil {
		for _, s := range servers {
			serverID[strings.ToLower(s.Name)] = s.ID
		}
	}
	// Grup dari file export (warna & grup kosong ikut dipulihkan).
	for _, g := range in.Groups {
		key := strings.ToLower(strings.TrimSpace(g.Name))
		if key == "" {
			continue
		}
		if _, ok := groupID[key]; ok {
			continue
		}
		color := g.Color
		if color == "" {
			color = "#2563EB"
		}
		if created, err := a.Store.CreateGroup(g.Name, color); err == nil {
			groupID[key] = created.ID
		}
	}

	imported, skipped := 0, []string{}
	type pendingJump struct{ serverID, jumpName string }
	var jumps []pendingJump

	for _, item := range in.Servers {
		payload := item.ServerInput
		if payload.GroupID == nil && strings.TrimSpace(item.GroupName) != "" {
			key := strings.ToLower(strings.TrimSpace(item.GroupName))
			id, ok := groupID[key]
			if !ok {
				color := "#2563EB"
				if created, err := a.Store.CreateGroup(item.GroupName, color); err == nil {
					id, ok = created.ID, true
					groupID[key] = id
				}
			}
			if ok {
				payload.GroupID = &id
			}
		}
		sv, err := a.Store.CreateServer(payload)
		if err != nil {
			label := payload.Name
			if label == "" {
				label = payload.Host
			}
			skipped = append(skipped, label+": "+err.Error())
			continue
		}
		imported++
		serverID[strings.ToLower(sv.Name)] = sv.ID
		if strings.TrimSpace(item.JumpName) != "" {
			jumps = append(jumps, pendingJump{serverID: sv.ID, jumpName: item.JumpName})
		}
	}

	// Fase kedua: sambungkan jump host setelah semua server ada.
	jumpsLinked := 0
	for _, j := range jumps {
		id, ok := serverID[strings.ToLower(strings.TrimSpace(j.jumpName))]
		if !ok || id == j.serverID {
			continue
		}
		if err := a.Store.SetServerJump(j.serverID, id); err == nil {
			jumpsLinked++
		}
	}

	writeJSON(w, 200, map[string]any{"imported": imported, "skipped": skipped, "jumpsLinked": jumpsLinked})
}

func (a *API) importSSHConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	readJSON(r, &in)
	p := in.Path
	if p == "" {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, ".ssh", "config")
	}
	p = sshx.ExpandHome(p)
	raw, err := os.ReadFile(p)
	if err != nil {
		writeErr(w, 400, "tidak bisa membaca "+p+": "+err.Error())
		return
	}
	hosts := parseSSHConfig(string(raw))
	imported, skipped := 0, []string{}
	for _, h := range hosts {
		in := store.ServerInput{Name: h.Alias, Host: h.HostName, Port: h.Port, Username: h.User,
			AuthType: "key", Tags: "import-sshconfig"}
		if in.Username == "" {
			in.Username = os.Getenv("USER")
		}
		if h.IdentityFile != "" {
			in.KeyPath = &h.IdentityFile
		} else {
			in.AuthType = "agent"
		}
		if _, err := a.Store.CreateServer(in); err != nil {
			skipped = append(skipped, h.Alias+": "+err.Error())
			continue
		}
		imported++
	}
	writeJSON(w, 200, map[string]any{"imported": imported, "skipped": skipped, "path": p, "found": len(hosts)})
}

type sshHost struct {
	Alias        string
	HostName     string
	User         string
	Port         int
	IdentityFile string
}

func parseSSHConfig(s string) []sshHost {
	var out []sshHost
	// nilai global (Host * / sebelum Host)
	global := map[string]string{}
	cur := map[string]string{}
	var aliases []string
	percent := func(v string) string {
		home, _ := os.UserHomeDir()
		v = strings.ReplaceAll(v, "%h", "")
		v = strings.ReplaceAll(v, "%r", "")
		return sshx.ExpandHome(strings.ReplaceAll(v, "~", home))
	}
	flush := func() {
		if aliases == nil {
			return
		}
		for _, a := range aliases {
			if strings.ContainsAny(a, "*?!") {
				continue
			}
			host := cur["hostname"]
			if host == "" {
				host = a
			}
			port := 22
			if v := cur["port"]; v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					port = n
				}
			}
			user := cur["user"]
			if user == "" {
				user = global["user"]
			}
			idf := cur["identityfile"]
			if idf == "" {
				idf = global["identityfile"]
			}
			out = append(out, sshHost{Alias: a, HostName: percent(host), User: user, Port: port, IdentityFile: percent(idf)})
		}
		aliases, cur = nil, map[string]string{}
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		key := strings.ToLower(f[0])
		val := strings.Join(f[1:], " ")
		if key == "host" {
			flush()
			aliases = f[1:]
			cur = map[string]string{}
			continue
		}
		if aliases == nil {
			global[key] = val
			continue
		}
		cur[key] = val
	}
	flush()
	return out
}

// ---------- batch ----------

func (a *API) execBatch(w http.ResponseWriter, r *http.Request) {
	var in batch.Request
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(in.Command) == "" {
		writeErr(w, 400, "perintah kosong")
		return
	}
	results, sum := batch.Run(r.Context(), a.Dialer, a.Store, in)
	writeJSON(w, 200, map[string]any{"results": results, "summary": sum})
}

func (a *API) logs(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := a.Store.ListLogs(n)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

// ---------- sftp ----------

func (a *API) sftpList(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("serverId")
	p := r.URL.Query().Get("path")
	abs, entries, err := a.Sftp.List(r.Context(), id, p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"path": abs, "entries": entries})
}

func (a *API) sftpMkdir(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ServerID string `json:"serverId"`
		Path     string `json:"path"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Sftp.Mkdir(r.Context(), in.ServerID, in.Path); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// safeUploadName mengambil nama dasar file dari header multipart sehingga nama
// seperti "../../etc/passwd" tidak bisa keluar dari direktori tujuan.
func safeUploadName(name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	base := path.Base(name)
	switch base {
	case "", ".", "..", "/":
		return "", errors.New("nama file tidak valid")
	}
	return base, nil
}

func (a *API) sftpUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, 400, "form tidak valid: "+err.Error())
		return
	}
	serverID := r.FormValue("serverId")
	dir := r.FormValue("path")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file tidak ditemukan: "+err.Error())
		return
	}
	defer file.Close()
	name, err := safeUploadName(hdr.Filename)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	remote := path.Join(dir, name)
	if dir == "" || dir == "." {
		remote = name
	}
	n, err := a.Sftp.Upload(r.Context(), serverID, remote, file)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "bytes": n, "remotePath": remote})
}

func (a *API) sftpDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("serverId")
	p := r.URL.Query().Get("path")
	if p == "" {
		writeErr(w, 400, "path kosong")
		return
	}
	// Stat dulu: jangan kirim header apa pun sebelum file dipastikan ada,
	// agar error tidak menghasilkan file korup tanpa pesan.
	fi, err := a.Sftp.Stat(r.Context(), id, p)
	if err != nil {
		writeErr(w, 404, "file tidak bisa diakses: "+err.Error())
		return
	}
	if fi.IsDir() {
		writeErr(w, 400, "path adalah direktori")
		return
	}
	name := path.Base(p)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "_")+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	if _, err := a.Sftp.Download(r.Context(), id, p, w); err != nil {
		// Header sudah terkirim; catat ke stderr (klien akan melihat file terpotong).
		fmt.Fprintln(os.Stderr, "download gagal:", err)
	}
}

// maxEditBytes membatasi ukuran file yang boleh dibuka di editor teks (1 MB).
const maxEditBytes = 1 << 20

func (a *API) sftpRead(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("serverId")
	p := r.URL.Query().Get("path")
	if p == "" {
		writeErr(w, 400, "path kosong")
		return
	}
	data, err := a.Sftp.Read(r.Context(), id, p, maxEditBytes)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"path": p, "content": string(data), "size": len(data)})
}

func (a *API) sftpWrite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ServerID string `json:"serverId"`
		Path     string `json:"path"`
		Content  string `json:"content"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.Path == "" {
		writeErr(w, 400, "path kosong")
		return
	}
	n, err := a.Sftp.Write(r.Context(), in.ServerID, in.Path, []byte(in.Content))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "bytes": n})
}

// ---------- metrics ----------
func (a *API) getMetrics(w http.ResponseWriter, r *http.Request) {
	ids := []string{}
	for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		writeJSON(w, 200, []metrics.Snapshot{})
		return
	}
	if len(ids) > 50 {
		ids = ids[:50]
	}
	writeJSON(w, 200, a.Metrics.Snapshot(r.Context(), ids))
}

// metricHistory mengembalikan riwayat metrik satu server (terlama -> terbaru)
// untuk grafik sparkline di Dashboard.
func (a *API) metricHistory(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("serverId"))
	if id == "" {
		writeErr(w, 400, "serverId wajib diisi")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 120
	}
	if limit > 1000 {
		limit = 1000
	}
	points, err := a.Store.ListMetricPoints(id, limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, points)
}

// services mengembalikan status unit systemd dan container Docker pada satu
// server. Keduanya dijalankan terpisah (paralel, koneksi masing-masing) supaya
// kegagalan salah satu — misalnya docker tidak terpasang — tidak menggagalkan
// bagian lain.
func (a *API) services(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("serverId"))
	if id == "" {
		writeErr(w, 400, "serverId wajib diisi")
		return
	}
	if a.Svcs == nil {
		writeErr(w, 500, "modul layanan tidak dikonfigurasi")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	var (
		wg      sync.WaitGroup
		systemd []svcs.Service
		contain []svcs.Container
		sysErr  error
		dockErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		systemd, sysErr = a.Svcs.Systemd(ctx, id)
	}()
	go func() {
		defer wg.Done()
		contain, dockErr = a.Svcs.Docker(ctx, id)
	}()
	wg.Wait()

	if systemd == nil {
		systemd = []svcs.Service{}
	}
	if contain == nil {
		contain = []svcs.Container{}
	}
	resp := map[string]any{
		"systemd":      systemd,
		"docker":       contain,
		"systemdError": "",
		"dockerError":  "",
	}
	if sysErr != nil {
		resp["systemdError"] = sysErr.Error()
	}
	if dockErr != nil {
		resp["dockerError"] = dockErr.Error()
	}
	writeJSON(w, 200, resp)
}

// ---------- alerts ----------
// listAlerts mengembalikan alert terbaru (default 50, maks 500).
func (a *API) listAlerts(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	list, err := a.Store.ListAlerts(limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

// alertsUnacked mengembalikan jumlah alert yang belum ditandai dibaca.
func (a *API) alertsUnacked(w http.ResponseWriter, r *http.Request) {
	n, err := a.Store.CountUnackedAlerts()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"count": n})
}

// ackAlerts menandai semua alert sebagai sudah dibaca.
func (a *API) ackAlerts(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.AckAllAlerts(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- settings ----------

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	al, _ := strconv.Atoi(a.Store.Setting("autoLockMin", "10"))
	th, _ := a.Store.GetAlertThresholds()
	writeJSON(w, 200, map[string]any{"theme": a.Store.Setting("theme", "light"), "autoLockMin": al, "alertThresholds": th})
}

func (a *API) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Theme           *string                `json:"theme"`
		AutoLockMin     *int                   `json:"autoLockMin"`
		AlertThresholds *store.AlertThresholds `json:"alertThresholds"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.Theme != nil {
		a.Store.SetSetting("theme", *in.Theme)
	}
	if in.AutoLockMin != nil {
		m := *in.AutoLockMin
		if m < 1 {
			m = 10
		}
		if m > 240 {
			m = 240
		}
		a.Store.SetSetting("autoLockMin", strconv.Itoa(m))
		a.Vault.SetAutoLock(time.Duration(m) * time.Minute)
	}
	if in.AlertThresholds != nil {
		if err := a.Store.SetAlertThresholds(*in.AlertThresholds); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- terminal ws ----------

func (a *API) termWS(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("serverId")
	sv, err := a.Store.GetServer(id)
	if err != nil {
		writeErr(w, 404, "server tidak ditemukan")
		return
	}
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	// Origin sudah divalidasi di withToken (loopback + port aplikasi).
	// OriginPatterns menjaga kompatibilitas cek bawaan library tanpa
	// mematikan verifikasi (tanpa InsecureSkipVerify).
	con, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{
			fmt.Sprintf("127.0.0.1:%d", a.Port),
			fmt.Sprintf("localhost:%d", a.Port),
		},
	})
	if err != nil {
		return
	}
	hub := a.hubInit()
	hub.add(con)
	defer hub.remove(con)
	term.Serve(r.Context(), con, a.Dialer, a.Store, sv, cols, rows)
}

// ---------- quick connect ----------

// quickTTL adalah umur maksimum kredensial quick connect di memory.
const quickTTL = 3 * time.Minute

// quickCreate menyimpan kredensial ad-hoc sementara lalu mengembalikan id
// sekali pakai untuk membuka terminal. Tidak ada data yang masuk inventaris.
func (a *API) quickCreate(w http.ResponseWriter, r *http.Request) {
	var in store.ServerInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(in.Host) == "" || strings.TrimSpace(in.Username) == "" || strings.TrimSpace(in.AuthType) == "" {
		writeErr(w, 400, "host, username, dan authType wajib diisi")
		return
	}
	id, err := randomHex(16)
	if err != nil {
		writeErr(w, 500, "gagal membuat id sesi")
		return
	}
	now := time.Now()
	a.quickMu.Lock()
	if a.quick == nil {
		a.quick = make(map[string]quickEntry)
	}
	// buang entri kedaluwarsa agar map tidak tumbuh
	for k, e := range a.quick {
		if now.After(e.exp) {
			delete(a.quick, k)
		}
	}
	a.quick[id] = quickEntry{in: in, exp: now.Add(quickTTL)}
	a.quickMu.Unlock()
	writeJSON(w, 200, map[string]any{"quickId": id})
}

// takeQuick mengambil sekaligus menghapus entri (single-use).
func (a *API) takeQuick(id string) (quickEntry, bool) {
	a.quickMu.Lock()
	defer a.quickMu.Unlock()
	e, ok := a.quick[id]
	if !ok {
		return quickEntry{}, false
	}
	delete(a.quick, id)
	if time.Now().After(e.exp) {
		return quickEntry{}, false
	}
	return e, true
}

func (a *API) quickTermWS(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("quickId")
	e, ok := a.takeQuick(id)
	if !ok {
		writeErr(w, 410, "sesi quick connect tidak ditemukan atau kedaluwarsa")
		return
	}
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	con, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{
			fmt.Sprintf("127.0.0.1:%d", a.Port),
			fmt.Sprintf("localhost:%d", a.Port),
		},
	})
	if err != nil {
		return
	}
	hub := a.hubInit()
	hub.add(con)
	defer hub.remove(con)
	term.ServeAdhoc(r.Context(), con, a.Dialer, e.in, cols, rows)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---------- SPA ----------

func (a *API) spa(w http.ResponseWriter, r *http.Request) {
	up := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if up == "" || up == "." {
		up = "index.html"
	}
	f, err := a.Assets.Open(up)
	if err != nil {
		up = "index.html"
		f, err = a.Assets.Open(up)
		if err != nil {
			http.Error(w, "UI belum dibangun (jalankan npm run build di web/)", 500)
			return
		}
	}
	defer f.Close()
	if ct := mime.TypeByExtension(filepath.Ext(up)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, up, time.Time{}, f.(io.ReadSeeker))
}

// ---------- helper ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("JSON tidak valid: %w", err)
	}
	return nil
}
