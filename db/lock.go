package db

import "github.com/akfaiz/quill/internal/dialect"

// LockForUpdate adds "FOR UPDATE" (Postgres, MySQL; ignored on SQLite).
func (b *Builder) LockForUpdate() *Builder { b.lock, b.lockRaw = dialect.LockUpdate, ""; return b }

// SharedLock adds "FOR SHARE" (Postgres) or "LOCK IN SHARE MODE" (MySQL); ignored on SQLite.
func (b *Builder) SharedLock() *Builder { b.lock, b.lockRaw = dialect.LockShared, ""; return b }

// Lock appends a raw locking clause such as "FOR NO KEY UPDATE" (caller-trusted
// SQL; ignored on SQLite).
func (b *Builder) Lock(clause string) *Builder { b.lock, b.lockRaw = dialect.LockRaw, clause; return b }

// UseIndex adds "USE INDEX (a, b)" (MySQL; ignored on Postgres and SQLite).
// Index names are emitted unquoted and validated against [a-zA-Z0-9_$].
func (b *Builder) UseIndex(index string) *Builder { b.hint = &indexHint{"use", index}; return b }

// ForceIndex adds "FORCE INDEX (a)" (MySQL) or "INDEXED BY a" (SQLite, one name).
func (b *Builder) ForceIndex(index string) *Builder { b.hint = &indexHint{"force", index}; return b }

// IgnoreIndex adds "IGNORE INDEX (a)" (MySQL; ignored elsewhere).
func (b *Builder) IgnoreIndex(index string) *Builder {
	b.hint = &indexHint{"ignore", index}
	return b
}
