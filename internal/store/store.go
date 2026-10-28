package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"bserver/internal/vault"
)

var (
	ErrNotFound  = errors.New("data tidak ditemukan")
	ErrInUse     = errors.New("masih dipakai")
	ErrBadInput  = errors.New("input tidak valid")
	ErrLocked    = vault.ErrLocked
	ErrDuplicate = errors.New("nama sudah dipakai")
)

type Store struct {
	db   *sql.DB
	v    *vault.Vault
	path string

	mu        sync.RWMutex
	sensitive []string
}

type Group struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	SortOrder int    `json:"sortOrder"`
}

type Server struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Host          string  `json:"host"`
	Port          int     `json:"port"`
	Username      string  `json:"username"`
	AuthType      string  `json:"authType"`
	GroupID       *string `json:"groupId"`
	GroupName     *string `json:"groupName"`
	CredentialID  *string `json:"credentialId"`
	JumpServerID  *string `json:"jumpServerId"`
	JumpName      *string `json:"jumpName"`
	Tags          string  `json:"tags"`
	Color         string  `json:"color"`
	Notes         string  `json:"notes"`
	Favorite      bool    `json:"favorite"`
	HostKeyAlgo   *string `json:"hostKeyAlgo"`
	HostKeyFp     *string `json:"hostKeyFp"`
	LastOKAt      *string `json:"lastOkAt"`
	LastLatencyMs *int    `json:"lastLatencyMs"`
	KeyPath       string  `json:"keyPath"`
	HasPassword   bool    `json:"hasPassword"`
	HasPassphrase bool    `json:"hasPassphrase"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

type ServerInput struct {
	Name         string  `json:"name"`
	Host         string  `json:"host"`
	Port         int     `json:"port"`
	Username     string  `json:"username"`
	AuthType     string  `json:"authType"`
	GroupID      *string `json:"groupId"`
	JumpServerID *string `json:"jumpServerId"`
	Tags         string  `json:"tags"`
	Color        string  `json:"color"`
	Notes        string  `json:"notes"`
	Favorite     bool    `json:"favorite"`
	Password     *string `json:"password"`
	KeyPath      *string `json:"keyPath"`
	Passphrase   *string `json:"passphrase"`
}

type Target struct {
	AuthType   string
	Username   string
	Password   string
	KeyPath    string
	Passphrase string
}

func New(path string, v *vault.Vault) (*Store, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db, v: v, path: path}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	s.secureFiles()
	return s, nil
}

// secureFiles memastikan file database (termasuk WAL/SHM) hanya bisa dibaca
// pemilik (0600), sesuai spesifikasi keamanan.
func (s *Store) secureFiles() {
	for _, p := range []string{s.path, s.path + "-wal", s.path + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			_ = os.Chmod(p, 0o600)
		}
	}
}

func (s *Store) Close() error {
	s.secureFiles()
	return s.db.Close()
}

// SetServerJump menetapkan jump host (dipakai saat impor dua fase).
func (s *Store) SetServerJump(id, jumpID string) error {
	_, err := s.db.Exec(`UPDATE servers SET jump_server_id=?, updated_at=? WHERE id=?`, jumpID, now(), id)
	return err
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT)`); err != nil {
		return err
	}
	applied := func(version int) bool {
		var n int
		s.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=?`, version).Scan(&n)
		return n > 0
	}
	if !applied(1) {
		if err := s.migrateV1(); err != nil {
			return err
		}
	}
	if !applied(2) {
		if err := s.migrateV2(); err != nil {
			return err
		}
	}
	if !applied(3) {
		if err := s.migrateV3(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateV1() error {
	ddl := []string{
		`CREATE TABLE groups(id TEXT PRIMARY KEY, name TEXT NOT NULL, parent_id TEXT, color TEXT, sort_order INTEGER DEFAULT 0, created_at TEXT)`,
		`CREATE TABLE credentials(id TEXT PRIMARY KEY, name TEXT, type TEXT, username TEXT, key_path TEXT, has_passphrase INTEGER DEFAULT 0, created_at TEXT)`,
		`CREATE TABLE servers(id TEXT PRIMARY KEY, name TEXT NOT NULL, host TEXT NOT NULL, port INTEGER DEFAULT 22, username TEXT,
			group_id TEXT, auth_type TEXT, credential_id TEXT, jump_server_id TEXT, tags TEXT, color TEXT, notes TEXT,
			favorite INTEGER DEFAULT 0, host_key_algo TEXT, host_key_fp TEXT, last_ok_at TEXT, last_latency_ms INTEGER,
			created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE secrets(id TEXT PRIMARY KEY, credential_id TEXT, kind TEXT, nonce BLOB, ciphertext BLOB, aad TEXT, updated_at TEXT)`,
		`CREATE TABLE known_hosts(host TEXT, port INTEGER, algo TEXT, fingerprint TEXT, first_seen TEXT, last_seen TEXT, PRIMARY KEY(host,port,algo))`,
		`CREATE TABLE command_log(id INTEGER PRIMARY KEY AUTOINCREMENT, ts TEXT, server_id TEXT, source TEXT, command TEXT, exit_code INTEGER, duration_ms INTEGER, output_excerpt TEXT)`,
		`CREATE TABLE snippets(id TEXT PRIMARY KEY, name TEXT, body TEXT, tags TEXT, created_at TEXT)`,
		`CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE INDEX idx_servers_name ON servers(name)`,
		`CREATE INDEX idx_servers_group ON servers(group_id)`,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, q := range ddl {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrasi: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(1, ?)`, now()); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// migrateV2 menambahkan riwayat metrik (sampling berkala oleh sampler).
func (s *Store) migrateV2() error {
	ddl := []string{
		`CREATE TABLE metric_history(id INTEGER PRIMARY KEY AUTOINCREMENT, server_id TEXT, ts TEXT,
			cpu_pct REAL, mem_used_kb INTEGER, mem_total_kb INTEGER,
			disk_used_kb INTEGER, disk_total_kb INTEGER, load1 REAL)`,
		`CREATE INDEX idx_metric_history_server ON metric_history(server_id, ts)`,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, q := range ddl {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrasi v2: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(2, ?)`, now()); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// migrateV3 menambahkan tabel alerts untuk peringatan ambang metrik lokal.
func (s *Store) migrateV3() error {
	ddl := []string{
		`CREATE TABLE alerts(id INTEGER PRIMARY KEY AUTOINCREMENT, server_id TEXT, ts TEXT, kind TEXT,
			value REAL, threshold REAL, message TEXT, acknowledged INTEGER DEFAULT 0)`,
		`CREATE INDEX idx_alerts_server ON alerts(server_id, ts)`,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, q := range ddl {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrasi v3: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(3, ?)`, now()); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func newID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// ---------- groups ----------

func (s *Store) ListGroups() ([]Group, error) {
	rows, err := s.db.Query(`SELECT id, name, COALESCE(color,''), COALESCE(sort_order,0) FROM groups ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Color, &g.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func (s *Store) CreateGroup(name, color string) (*Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadInput
	}
	g := Group{ID: newID("grp_"), Name: name, Color: color}
	_, err := s.db.Exec(`INSERT INTO groups(id,name,color,sort_order,created_at) VALUES(?,?,?,0,?)`, g.ID, g.Name, g.Color, now())
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *Store) UpdateGroup(id, name, color string) error {
	res, err := s.db.Exec(`UPDATE groups SET name=?, color=? WHERE id=?`, name, color, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteGroup(id string) error {
	if _, err := s.db.Exec(`UPDATE servers SET group_id=NULL WHERE group_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM groups WHERE id=?`, id)
	return err
}

// ---------- servers ----------

const serverSelect = `SELECT s.id,s.name,s.host,s.port,s.username,COALESCE(s.auth_type,''),s.group_id,g.name,s.credential_id,
	s.jump_server_id, j.name, COALESCE(s.tags,''), COALESCE(s.color,''), COALESCE(s.notes,''), COALESCE(s.favorite,0),
	s.host_key_algo, s.host_key_fp, s.last_ok_at, s.last_latency_ms, COALESCE(c.key_path,''),
	EXISTS(SELECT 1 FROM secrets x WHERE x.credential_id=s.credential_id AND x.kind='password'),
	EXISTS(SELECT 1 FROM secrets x WHERE x.credential_id=s.credential_id AND x.kind='passphrase'),
	COALESCE(s.created_at,''), COALESCE(s.updated_at,'')
	FROM servers s LEFT JOIN groups g ON g.id=s.group_id
	LEFT JOIN servers j ON j.id=s.jump_server_id
	LEFT JOIN credentials c ON c.id=s.credential_id`

func scanServer(sc interface{ Scan(...any) error }) (*Server, error) {
	var sv Server
	var gid, gname, cred, jump, jumpName, algo, fp, lastOK, keyPath sql.NullString
	var lat sql.NullInt64
	if err := sc.Scan(&sv.ID, &sv.Name, &sv.Host, &sv.Port, &sv.Username, &sv.AuthType, &gid, &gname, &cred,
		&jump, &jumpName, &sv.Tags, &sv.Color, &sv.Notes, &sv.Favorite, &algo, &fp, &lastOK, &lat, &keyPath,
		&sv.HasPassword, &sv.HasPassphrase, &sv.CreatedAt, &sv.UpdatedAt); err != nil {
		return nil, err
	}
	ptr := func(n sql.NullString) *string {
		if n.Valid {
			v := n.String
			return &v
		}
		return nil
	}
	sv.GroupID, sv.GroupName, sv.CredentialID = ptr(gid), ptr(gname), ptr(cred)
	sv.JumpServerID, sv.JumpName = ptr(jump), ptr(jumpName)
	sv.HostKeyAlgo, sv.HostKeyFp, sv.LastOKAt = ptr(algo), ptr(fp), ptr(lastOK)
	sv.KeyPath = keyPath.String
	if lat.Valid {
		v := int(lat.Int64)
		sv.LastLatencyMs = &v
	}
	return &sv, nil
}

func (s *Store) ListServers(q string) ([]Server, error) {
	sqlq := serverSelect
	var args []any
	if q = strings.TrimSpace(q); q != "" {
		sqlq += ` WHERE s.name LIKE ? OR s.host LIKE ? OR s.username LIKE ? OR COALESCE(s.tags,'') LIKE ?`
		l := "%" + q + "%"
		args = append(args, l, l, l, l)
	}
	sqlq += ` ORDER BY s.favorite DESC, s.name COLLATE NOCASE`
	rows, err := s.db.Query(sqlq, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		sv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sv)
	}
	return out, nil
}

func (s *Store) GetServer(id string) (*Server, error) {
	row := s.db.QueryRow(serverSelect+` WHERE s.id=?`, id)
	sv, err := scanServer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sv, err
}

func (in *ServerInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.Username = strings.TrimSpace(in.Username)
	in.AuthType = strings.TrimSpace(in.AuthType)
	if in.Name == "" {
		in.Name = in.Host
	}
	if in.Host == "" || in.Username == "" {
		return ErrBadInput
	}
	if in.Port <= 0 || in.Port > 65535 {
		in.Port = 22
	}
	if in.AuthType == "" {
		in.AuthType = "key"
	}
	switch in.AuthType {
	case "password", "key", "agent":
	default:
		return ErrBadInput
	}
	return nil
}

func (s *Store) storeSecret(tx *sql.Tx, credID, kind, plain string) error {
	if plain == "" {
		return nil
	}
	sec, err := s.v.EncryptSecret(credID+"/"+kind, kind, []byte(plain))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO secrets(id,credential_id,kind,nonce,ciphertext,aad,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET nonce=excluded.nonce, ciphertext=excluded.ciphertext, updated_at=excluded.updated_at`,
		credID+"/"+kind, credID, kind, sec.Nonce, sec.Ciphertext, sec.AAD, now())
	return err
}

func (s *Store) GetSecret(credID, kind string) (string, error) {
	var nonce, ct []byte
	err := s.db.QueryRow(`SELECT nonce,ciphertext FROM secrets WHERE credential_id=? AND kind=?`, credID, kind).Scan(&nonce, &ct)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	b, err := s.v.DecryptSecret(credID+"/"+kind, kind, vault.Secret{Nonce: nonce, Ciphertext: ct})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Store) CreateServer(in ServerInput) (*Server, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var credID any
	if in.AuthType != "agent" {
		id := newID("cred_")
		keyPath := ""
		if in.KeyPath != nil {
			keyPath = *in.KeyPath
		}
		hasPass := 0
		if in.AuthType == "key" && in.Passphrase != nil && *in.Passphrase != "" {
			hasPass = 1
		}
		if _, err := tx.Exec(`INSERT INTO credentials(id,name,type,username,key_path,has_passphrase,created_at) VALUES(?,?,?,?,?,?,?)`,
			id, in.Name, in.AuthType, in.Username, keyPath, hasPass, now()); err != nil {
			return nil, err
		}
		if in.AuthType == "password" && in.Password != nil {
			if err := s.storeSecret(tx, id, "password", *in.Password); err != nil {
				return nil, err
			}
		}
		if in.AuthType == "key" && in.Passphrase != nil {
			if err := s.storeSecret(tx, id, "passphrase", *in.Passphrase); err != nil {
				return nil, err
			}
		}
		credID = id
	}

	sid := newID("srv_")
	_, err = tx.Exec(`INSERT INTO servers(id,name,host,port,username,auth_type,group_id,credential_id,jump_server_id,tags,color,notes,favorite,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sid, in.Name, in.Host, in.Port, in.Username, in.AuthType, in.GroupID, credID, nilIfEmpty(in.JumpServerID),
		in.Tags, in.Color, in.Notes, in.Favorite, now(), now())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetServer(sid)
}

func nilIfEmpty(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

func (s *Store) UpdateServer(id string, in ServerInput) (*Server, error) {
	cur, err := s.GetServer(id)
	if err != nil {
		return nil, err
	}
	if err := in.normalize(); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	credID := ""
	if cur.CredentialID != nil {
		credID = *cur.CredentialID
	}
	needsCred := in.AuthType != "agent"
	if credID == "" && needsCred {
		credID = newID("cred_")
		if _, err := tx.Exec(`INSERT INTO credentials(id,name,type,username,key_path,has_passphrase,created_at) VALUES(?,?,?,?,?,?,?)`,
			credID, in.Name, in.AuthType, in.Username, "", 0, now()); err != nil {
			return nil, err
		}
	}
	if credID != "" {
		keyPath := cur.KeyPath
		if in.KeyPath != nil {
			keyPath = *in.KeyPath
		}
		if _, err := tx.Exec(`UPDATE credentials SET name=?, type=?, username=?, key_path=? WHERE id=?`,
			in.Name, in.AuthType, in.Username, keyPath, credID); err != nil {
			return nil, err
		}
		// Bersihkan secret yang tidak relevan dengan metode auth baru, agar
		// pindah password/key -> agent tidak menyisakan secret lama.
		// Catatan: string kosong dari UI berarti "tidak diubah", jadi secret
		// hanya dihapus saat metode auth berubah (bukan saat field dikosongkan).
		switch in.AuthType {
		case "agent":
			if _, err := tx.Exec(`DELETE FROM secrets WHERE credential_id=?`, credID); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(`UPDATE credentials SET has_passphrase=0 WHERE id=?`, credID); err != nil {
				return nil, err
			}
		case "password":
			if _, err := tx.Exec(`DELETE FROM secrets WHERE credential_id=? AND kind='passphrase'`, credID); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(`UPDATE credentials SET has_passphrase=0 WHERE id=?`, credID); err != nil {
				return nil, err
			}
		case "key":
			if _, err := tx.Exec(`DELETE FROM secrets WHERE credential_id=? AND kind='password'`, credID); err != nil {
				return nil, err
			}
		}
		if in.AuthType == "password" && in.Password != nil {
			if err := s.storeSecret(tx, credID, "password", *in.Password); err != nil {
				return nil, err
			}
		}
		if in.AuthType == "key" && in.Passphrase != nil {
			if err := s.storeSecret(tx, credID, "passphrase", *in.Passphrase); err != nil {
				return nil, err
			}
			has := 0
			if *in.Passphrase != "" {
				has = 1
			}
			if _, err := tx.Exec(`UPDATE credentials SET has_passphrase=? WHERE id=?`, has, credID); err != nil {
				return nil, err
			}
		}
	}
	var credArg any
	if credID != "" {
		credArg = credID
	}
	_, err = tx.Exec(`UPDATE servers SET name=?,host=?,port=?,username=?,auth_type=?,group_id=?,credential_id=?,jump_server_id=?,tags=?,color=?,notes=?,favorite=?,updated_at=? WHERE id=?`,
		in.Name, in.Host, in.Port, in.Username, in.AuthType, in.GroupID, credArg, nilIfEmpty(in.JumpServerID),
		in.Tags, in.Color, in.Notes, in.Favorite, now(), id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetServer(id)
}

func (s *Store) DeleteServer(id string) error {
	sv, err := s.GetServer(id)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if sv.CredentialID != nil {
		if _, err := tx.Exec(`DELETE FROM secrets WHERE credential_id=?`, *sv.CredentialID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM credentials WHERE id=?`, *sv.CredentialID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE servers SET jump_server_id=NULL WHERE jump_server_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM servers WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Resolve mengembalikan data auth siap pakai untuk dial.
func (s *Store) Resolve(sv *Server) (*Target, error) {
	t := &Target{AuthType: sv.AuthType, Username: sv.Username, KeyPath: sv.KeyPath}
	if sv.CredentialID != nil && (sv.AuthType == "password" || sv.AuthType == "key") {
		pw, err := s.GetSecret(*sv.CredentialID, "password")
		if err != nil {
			return nil, err
		}
		pp, err := s.GetSecret(*sv.CredentialID, "passphrase")
		if err != nil {
			return nil, err
		}
		t.Password, t.Passphrase = pw, pp
	}
	return t, nil
}

func (s *Store) SetServerStatus(id string, latencyMs int, ok bool) {
	if ok {
		s.db.Exec(`UPDATE servers SET last_ok_at=?, last_latency_ms=? WHERE id=?`, now(), latencyMs, id)
	}
}

func (s *Store) SetHostKey(serverID, algo, fp string) {
	s.db.Exec(`UPDATE servers SET host_key_algo=?, host_key_fp=? WHERE id=?`, algo, fp, serverID)
}

// ---------- known_hosts (TOFU) ----------

func (s *Store) CheckHostKey(host string, port int, algo, fp string) (string, error) {
	var stored string
	err := s.db.QueryRow(`SELECT fingerprint FROM known_hosts WHERE host=? AND port=? AND algo=?`, host, port, algo).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		// INSERT OR IGNORE: dua koneksi paralel (mis. systemd + docker) bisa
		// sama-sama melihat row belum ada lalu insert bersamaan — hindari
		// UNIQUE constraint violation pada race TOFU.
		_, err = s.db.Exec(`INSERT OR IGNORE INTO known_hosts(host,port,algo,fingerprint,first_seen,last_seen) VALUES(?,?,?,?,?,?)`,
			host, port, algo, fp, now(), now())
		return "new", err
	}
	if err != nil {
		return "", err
	}
	s.db.Exec(`UPDATE known_hosts SET last_seen=? WHERE host=? AND port=? AND algo=?`, now(), host, port, algo)
	if stored != fp {
		return "changed", nil
	}
	return "match", nil
}

// ForgetHostKey menghapus pin host key (dipakai bila user menerima perubahan key).
func (s *Store) ForgetHostKey(host string, port int) error {
	_, err := s.db.Exec(`DELETE FROM known_hosts WHERE host=? AND port=?`, host, port)
	return err
}

// ---------- snippets ----------

// Snippet adalah perintah pendek yang sering dipakai, disimpan lokal.
type Snippet struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Body      string `json:"body"`
	Tags      string `json:"tags"`
	CreatedAt string `json:"createdAt"`
}

const snippetSelect = `SELECT id, COALESCE(name,''), COALESCE(body,''), COALESCE(tags,''), COALESCE(created_at,'') FROM snippets`

func scanSnippet(sc interface{ Scan(...any) error }) (*Snippet, error) {
	var sn Snippet
	if err := sc.Scan(&sn.ID, &sn.Name, &sn.Body, &sn.Tags, &sn.CreatedAt); err != nil {
		return nil, err
	}
	return &sn, nil
}

func (s *Store) ListSnippets() ([]Snippet, error) {
	rows, err := s.db.Query(snippetSelect + ` ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Snippet{}
	for rows.Next() {
		sn, err := scanSnippet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sn)
	}
	return out, rows.Err()
}

func (s *Store) CreateSnippet(name, body, tags string) (*Snippet, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadInput
	}
	sn := Snippet{ID: newID("snp_"), Name: name, Body: body, Tags: strings.TrimSpace(tags), CreatedAt: now()}
	_, err := s.db.Exec(`INSERT INTO snippets(id,name,body,tags,created_at) VALUES(?,?,?,?,?)`,
		sn.ID, sn.Name, sn.Body, sn.Tags, sn.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &sn, nil
}

func (s *Store) UpdateSnippet(id, name, body, tags string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrBadInput
	}
	res, err := s.db.Exec(`UPDATE snippets SET name=?, body=?, tags=? WHERE id=?`,
		name, body, strings.TrimSpace(tags), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSnippet(id string) error {
	res, err := s.db.Exec(`DELETE FROM snippets WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- settings ----------

func (s *Store) Setting(key, def string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ---------- command log ----------

type LogEntry struct {
	ID         int64  `json:"id"`
	TS         string `json:"ts"`
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	Source     string `json:"source"`
	Command    string `json:"command"`
	ExitCode   int    `json:"exitCode"`
	DurationMs int64  `json:"durationMs"`
	Excerpt    string `json:"excerpt"`
}

// SetSensitive mendaftarkan nilai rahasia runtime (mis. token sesi) yang ikut
// diredaksi saat ditulis ke command_log.
func (s *Store) SetSensitive(values ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sensitive = s.sensitive[:0]
	for _, v := range values {
		if len(v) >= 4 {
			s.sensitive = append(s.sensitive, v)
		}
	}
}

func (s *Store) AddLog(serverID, source, command string, exitCode int, durMs int64, excerpt string) {
	s.mu.RLock()
	cmd := RedactWith(command, s.sensitive...)
	exc := RedactWith(excerpt, s.sensitive...)
	s.mu.RUnlock()
	s.db.Exec(`INSERT INTO command_log(ts,server_id,source,command,exit_code,duration_ms,output_excerpt) VALUES(?,?,?,?,?,?,?)`,
		now(), serverID, source, cmd, exitCode, durMs, exc)
}

func (s *Store) ListLogs(limit int) ([]LogEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT l.id, l.ts, COALESCE(l.server_id,''), COALESCE(s.name,''), COALESCE(l.source,''),
		COALESCE(l.command,''), COALESCE(l.exit_code,0), COALESCE(l.duration_ms,0), COALESCE(l.output_excerpt,'')
		FROM command_log l LEFT JOIN servers s ON s.id=l.server_id ORDER BY l.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.ServerID, &e.ServerName, &e.Source, &e.Command, &e.ExitCode, &e.DurationMs, &e.Excerpt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// ---------- metric history ----------

// MetricPoint adalah satu sampel metrik server yang disimpan untuk grafik
// historis (diambil berkala oleh sampler).
type MetricPoint struct {
	TS          string  `json:"ts"`
	CPU         float64 `json:"cpuPct"`
	MemUsedKb   int64   `json:"memUsedKb"`
	MemTotalKb  int64   `json:"memTotalKb"`
	DiskUsedKb  int64   `json:"diskUsedKb"`
	DiskTotalKb int64   `json:"diskTotalKb"`
	Load1       float64 `json:"load1"`
}

// metricRetention adalah umur maksimum riwayat metrik yang disimpan.
const metricRetention = 7 * 24 * time.Hour

// InsertMetricPoint menyimpan satu sampel metrik dan membuang data yang lebih
// tua dari masa retensi agar tabel tidak menumpuk.
func (s *Store) InsertMetricPoint(serverID string, p MetricPoint) error {
	if p.TS == "" {
		p.TS = now()
	}
	if _, err := s.db.Exec(`INSERT INTO metric_history(server_id,ts,cpu_pct,mem_used_kb,mem_total_kb,disk_used_kb,disk_total_kb,load1)
		VALUES(?,?,?,?,?,?,?,?)`,
		serverID, p.TS, p.CPU, p.MemUsedKb, p.MemTotalKb, p.DiskUsedKb, p.DiskTotalKb, p.Load1); err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-metricRetention).Format(time.RFC3339)
	if _, err := s.db.Exec(`DELETE FROM metric_history WHERE ts < ?`, cutoff); err != nil {
		return err
	}
	return nil
}

// ListMetricPoints mengembalikan `limit` sampel terakhir untuk satu server,
// diurutkan dari terlama ke terbaru (siap dipakai sebagai grafik).
func (s *Store) ListMetricPoints(serverID string, limit int) ([]MetricPoint, error) {
	if limit <= 0 {
		limit = 120
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT ts, COALESCE(cpu_pct,0), COALESCE(mem_used_kb,0), COALESCE(mem_total_kb,0),
		COALESCE(disk_used_kb,0), COALESCE(disk_total_kb,0), COALESCE(load1,0)
		FROM (SELECT * FROM metric_history WHERE server_id=? ORDER BY ts DESC, id DESC LIMIT ?)
		ORDER BY ts ASC`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricPoint{}
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.TS, &p.CPU, &p.MemUsedKb, &p.MemTotalKb, &p.DiskUsedKb, &p.DiskTotalKb, &p.Load1); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------- alerts ----------

// Alert adalah satu peringatan lokal yang tercatat saat metrik melewati ambang.
type Alert struct {
	ID           int64   `json:"id"`
	ServerID     string  `json:"serverId"`
	ServerName   string  `json:"serverName"`
	TS           string  `json:"ts"`
	Kind         string  `json:"kind"`
	Value        float64 `json:"value"`
	Threshold    float64 `json:"threshold"`
	Message      string  `json:"message"`
	Acknowledged bool    `json:"acknowledged"`
}

// AlertThresholds adalah ambang persentase per metrik. Nilai 0 berarti mati.
type AlertThresholds struct {
	CPUPct  float64 `json:"cpuPct"`
	MemPct  float64 `json:"memPct"`
	DiskPct float64 `json:"diskPct"`
}

const alertThresholdsKey = "alertThresholds"

// GetAlertThresholds membaca ambang alert dari settings (default semua 0).
func (s *Store) GetAlertThresholds() (AlertThresholds, error) {
	var t AlertThresholds
	raw := s.Setting(alertThresholdsKey, "")
	if raw == "" {
		return t, nil
	}
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return AlertThresholds{}, nil // data rusak: anggap semua mati
	}
	return t, nil
}

// SetAlertThresholds menyimpan ambang alert sebagai JSON di settings.
func (s *Store) SetAlertThresholds(t AlertThresholds) error {
	if t.CPUPct < 0 {
		t.CPUPct = 0
	}
	if t.MemPct < 0 {
		t.MemPct = 0
	}
	if t.DiskPct < 0 {
		t.DiskPct = 0
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return s.SetSetting(alertThresholdsKey, string(b))
}

// InsertAlert menyimpan alert baru. Dedup: bila sudah ada alert belum
// di-acknowledge untuk server+kind yang sama, alert baru dilewati agar daftar
// tidak dibanjiri alert berulang setiap siklus sampling.
func (s *Store) InsertAlert(serverID, kind string, value, threshold float64, message string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM alerts WHERE server_id=? AND kind=? AND acknowledged=0`,
		serverID, kind).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO alerts(server_id,ts,kind,value,threshold,message,acknowledged) VALUES(?,?,?,?,?,?,0)`,
		serverID, now(), kind, value, threshold, message)
	return err
}

// ListAlerts mengembalikan alert terbaru (default 50) beserta nama server.
func (s *Store) ListAlerts(limit int) ([]Alert, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT a.id, COALESCE(a.server_id,''), COALESCE(sv.name,''), COALESCE(a.ts,''),
		COALESCE(a.kind,''), COALESCE(a.value,0), COALESCE(a.threshold,0), COALESCE(a.message,''),
		COALESCE(a.acknowledged,0)
		FROM alerts a LEFT JOIN servers sv ON sv.id=a.server_id ORDER BY a.ts DESC, a.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var al Alert
		if err := rows.Scan(&al.ID, &al.ServerID, &al.ServerName, &al.TS, &al.Kind, &al.Value, &al.Threshold,
			&al.Message, &al.Acknowledged); err != nil {
			return nil, err
		}
		out = append(out, al)
	}
	return out, rows.Err()
}

// CountUnackedAlerts menghitung alert yang belum ditandai dibaca.
func (s *Store) CountUnackedAlerts() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM alerts WHERE acknowledged=0`).Scan(&n)
	return n, err
}

// AckAllAlerts menandai semua alert sebagai sudah dibaca.
func (s *Store) AckAllAlerts() error {
	_, err := s.db.Exec(`UPDATE alerts SET acknowledged=1`)
	return err
}
