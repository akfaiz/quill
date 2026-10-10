package db

import "github.com/akfaiz/quill/internal/dialect"

// LockForUpdate adds "FOR UPDATE" (Postgres, MySQL; ignored on SQLite).
//
// Example:
//
//	conn.Table("accounts").Where("id", 1).LockForUpdate()
//	// Postgres: SELECT * FROM "accounts" WHERE "id" = $1 FOR UPDATE   args: [1]
//	// MySQL: SELECT * FROM `accounts` WHERE `id` = ? FOR UPDATE   args: [1]
func (b *Builder) LockForUpdate() *Builder { b.lock, b.lockRaw = dialect.LockUpdate, ""; return b }

// SharedLock adds "FOR SHARE" (Postgres) or "LOCK IN SHARE MODE" (MySQL); ignored on SQLite.
//
// Example:
//
//	conn.Table("accounts").Where("id", 1).SharedLock()
//	// Postgres: SELECT * FROM "accounts" WHERE "id" = $1 FOR SHARE   args: [1]
//	// MySQL: SELECT * FROM `accounts` WHERE `id` = ? LOCK IN SHARE MODE   args: [1]
func (b *Builder) SharedLock() *Builder { b.lock, b.lockRaw = dialect.LockShared, ""; return b }

// Lock appends a raw locking clause such as "FOR NO KEY UPDATE" (caller-trusted
// SQL; ignored on SQLite).
//
// Example:
//
//	conn.Table("jobs").Where("state", "queued").Limit(1).Lock("FOR NO KEY UPDATE")
//	// SELECT * FROM "jobs" WHERE "state" = $1 LIMIT 1 FOR NO KEY UPDATE   args: [queued]
func (b *Builder) Lock(clause string) *Builder { b.lock, b.lockRaw = dialect.LockRaw, clause; return b }

// UseIndex adds "USE INDEX (a, b)" (MySQL; ignored on Postgres and SQLite).
// Index names are emitted unquoted and validated against [a-zA-Z0-9_$].
//
// Example:
//
//	conn.Table("users").UseIndex("idx_email").Where("active", true)
//	// SELECT * FROM `users` USE INDEX (idx_email) WHERE `active` = ?   args: [true]
func (b *Builder) UseIndex(index string) *Builder { b.hint = &indexHint{"use", index}; return b }

// ForceIndex adds "FORCE INDEX (a)" (MySQL) or "INDEXED BY a" (SQLite, one name).
//
// Example:
//
//	conn.Table("users").ForceIndex("idx_email").Where("active", true)
//	// MySQL: SELECT * FROM `users` FORCE INDEX (idx_email) WHERE `active` = ?   args: [true]
//	// SQLite: SELECT * FROM "users" INDEXED BY idx_email WHERE "active" = ?   args: [true]
func (b *Builder) ForceIndex(index string) *Builder { b.hint = &indexHint{"force", index}; return b }

// IgnoreIndex adds "IGNORE INDEX (a)" (MySQL; ignored elsewhere).
//
// Example:
//
//	conn.Table("users").IgnoreIndex("idx_email").Where("active", true)
//	// SELECT * FROM `users` IGNORE INDEX (idx_email) WHERE `active` = ?   args: [true]
func (b *Builder) IgnoreIndex(index string) *Builder {
	b.hint = &indexHint{"ignore", index}
	return b
}
