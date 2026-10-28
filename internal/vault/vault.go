package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

var (
	ErrLocked       = errors.New("vault terkunci")
	ErrWrongPass    = errors.New("passphrase salah")
	ErrNotExists    = errors.New("vault belum dibuat")
	ErrExists       = errors.New("vault sudah ada")
	ErrShortPass    = errors.New("passphrase minimal 8 karakter")
	ErrVaultCorrupt = errors.New("file vault tidak valid")
)

const verifierPlain = "bServer-vault-v1"

type KDF struct {
	Algo string `json:"algo"`
	Salt string `json:"salt"`
	M    uint32 `json:"m"`
	T    uint32 `json:"t"`
	P    uint8  `json:"p"`
}

type Sealed struct {
	N string `json:"n"`
	C string `json:"c"`
}

type Header struct {
	V         int    `json:"v"`
	KDF       KDF    `json:"kdf"`
	Verifier  Sealed `json:"verifier"`
	CreatedAt string `json:"created_at"`
}

type Vault struct {
	mu           sync.Mutex
	path         string
	hdr          Header
	key          []byte
	unlocked     bool
	lastActivity time.Time
	autoLock     time.Duration
}

func New(path string) *Vault {
	return &Vault{path: path, autoLock: 10 * time.Minute}
}

func (v *Vault) SetAutoLock(d time.Duration) {
	v.mu.Lock()
	v.autoLock = d
	v.mu.Unlock()
}

func (v *Vault) AutoLock() time.Duration {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.autoLock
}

func (v *Vault) Exists() bool {
	_, err := os.Stat(v.path)
	return err == nil
}

func (v *Vault) Unlocked() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.unlocked
}

// Touch memperbarui waktu aktivitas terakhir dan melakukan auto-lock bila idle.
func (v *Vault) Touch() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.unlocked && v.autoLock > 0 && time.Since(v.lastActivity) > v.autoLock {
		v.lockLocked()
		return
	}
	v.lastActivity = time.Now()
}

func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lockLocked()
}

func (v *Vault) lockLocked() {
	zeroize(v.key)
	v.key = nil
	v.unlocked = false
}

// zeroize menimpa isi slice kunci dengan nol agar kunci lama tidak tertinggal
// di memori setelah diganti (Unlock berulang) atau saat lock.
func zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func derive(pass string, k KDF) []byte {
	salt, _ := base64.StdEncoding.DecodeString(k.Salt)
	return argon2.IDKey([]byte(pass), salt, k.T, k.M, k.P, 32)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(key []byte, plain []byte, aad []byte) (Sealed, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return Sealed{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Sealed{}, err
	}
	ct := gcm.Seal(nil, nonce, plain, aad)
	return Sealed{
		N: base64.StdEncoding.EncodeToString(nonce),
		C: base64.StdEncoding.EncodeToString(ct),
	}, nil
}

func open(key []byte, s Sealed, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(s.N)
	if err != nil {
		return nil, ErrVaultCorrupt
	}
	ct, err := base64.StdEncoding.DecodeString(s.C)
	if err != nil {
		return nil, ErrVaultCorrupt
	}
	return gcm.Open(nil, nonce, ct, aad)
}

func (v *Vault) Create(pass string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.Exists() {
		return ErrExists
	}
	if len(pass) < 8 {
		return ErrShortPass
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	k := KDF{Algo: "argon2id", Salt: base64.StdEncoding.EncodeToString(salt), M: 64 * 1024, T: 3, P: 4}
	key := derive(pass, k)
	ver, err := seal(key, []byte(verifierPlain), nil)
	if err != nil {
		return err
	}
	hdr := Header{V: 1, KDF: k, Verifier: ver, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := v.writeHeader(hdr); err != nil {
		return err
	}
	v.hdr = hdr
	v.key = key
	v.unlocked = true
	v.lastActivity = time.Now()
	return nil
}

func (v *Vault) writeHeader(h Header) error {
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}

func (v *Vault) Unlock(pass string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.Exists() {
		return ErrNotExists
	}
	raw, err := os.ReadFile(v.path)
	if err != nil {
		return err
	}
	var h Header
	if err := json.Unmarshal(raw, &h); err != nil || h.V != 1 {
		return ErrVaultCorrupt
	}
	key := derive(pass, h.KDF)
	plain, err := open(key, h.Verifier, nil)
	if err != nil || subtle.ConstantTimeCompare(plain, []byte(verifierPlain)) != 1 {
		return ErrWrongPass
	}
	// Kunci lama (bila vault sudah terbuka) di-zeroize sebelum diganti.
	zeroize(v.key)
	v.hdr = h
	v.key = key
	v.unlocked = true
	v.lastActivity = time.Now()
	return nil
}

func aad(id, kind string) []byte {
	return []byte(fmt.Sprintf("%s|%s|1", id, kind))
}

type Secret struct {
	Nonce      []byte
	Ciphertext []byte
	AAD        string
}

// EncryptSecret mengenkripsi secret dengan AAD = id|kind|schemaVersion.
func (v *Vault) EncryptSecret(id, kind string, plain []byte) (Secret, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return Secret{}, ErrLocked
	}
	a := aad(id, kind)
	gcm, err := newGCM(v.key)
	if err != nil {
		return Secret{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Secret{}, err
	}
	ct := gcm.Seal(nil, nonce, plain, a)
	return Secret{Nonce: nonce, Ciphertext: ct, AAD: string(a)}, nil
}

func (v *Vault) DecryptSecret(id, kind string, s Secret) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	gcm, err := newGCM(v.key)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, s.Nonce, s.Ciphertext, aad(id, kind))
}
