package db

import (
	"github.com/akfaiz/quill/internal/dialect"
)

// LockForUpdate adds "for update" (Postgres, MySQL; ignored on SQLite).
func (b *Builder) LockForUpdate() *Builder { b.lock, b.lockRaw = dialect.LockUpdate, ""; return b }

// SharedLock adds "for share" (Postgres) or "lock in share mode" (MySQL); ignored on SQLite.
func (b *Builder) SharedLock() *Builder { b.lock, b.lockRaw = dialect.LockShared, ""; return b }

// Lock appends a raw locking clause such as "for no key update" (caller-trusted
// SQL; ignored on SQLite).
func (b *Builder) Lock(clause string) *Builder { b.lock, b.lockRaw = dialect.LockRaw, clause; return b }

// UseIndex adds "use index (a, b)" (MySQL; ignored on Postgres and SQLite).
// Index names are emitted unquoted and validated against [a-zA-Z0-9_$].
func (b *Builder) UseIndex(index string) *Builder { b.hint = &indexHint{"use", index}; return b }

// ForceIndex adds "force index (a)" (MySQL) or "indexed by a" (SQLite, one name).
func (b *Builder) ForceIndex(index string) *Builder { b.hint = &indexHint{"force", index}; return b }

// IgnoreIndex adds "ignore index (a)" (MySQL; ignored elsewhere).
func (b *Builder) IgnoreIndex(index string) *Builder {
	b.hint = &indexHint{"ignore", index}
	return b
}
