package store

import (
	"regexp"
	"strings"
)

var (
	// Blok private key (OpenSSH/PEM/RSA/EC/dll).
	rePrivateKey = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)
	// Pasangan kunci/nilai rahasia dalam JSON, mis. `"password":"rahasia"`.
	reJSONSecret = regexp.MustCompile(`(?i)("(?:password|passphrase|passwd|secret|token|api[_-]?key)"\s*:\s*")([^"\\]*(?:\\.[^"\\]*)*)(")`)
	// Bentuk `password=...`, `passphrase: ...`, `token=...`, termasuk flag CLI.
	reKVSecret = regexp.MustCompile(`(?i)(\b(?:password|passphrase|passwd|secret|token|api[_-]?key)\b\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s;,]+)`)
)

const redacted = "[REDACTED]"

// Redact menyamarkan secret yang umum muncul di command/output.
func Redact(s string) string { return RedactWith(s) }

// RedactWith seperti Redact, tetapi juga menyamarkan nilai tambahan (mis. token
// sesi atau passphrase aktif) yang dioper pemanggil.
func RedactWith(s string, extras ...string) string {
	if s == "" {
		return s
	}
	out := rePrivateKey.ReplaceAllString(s, redacted)
	out = reJSONSecret.ReplaceAllString(out, "${1}"+redacted+"${3}")
	out = reKVSecret.ReplaceAllString(out, "${1}"+redacted)
	for _, v := range extras {
		// Nilai sangat pendek berisiko merusak teks biasa; token/passphrase
		// yang berarti hampir selalu >= 4 karakter.
		if len(v) >= 4 {
			out = strings.ReplaceAll(out, v, redacted)
		}
	}
	return out
}
