package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/akfaiz/quill/db"
)

func TestLockAndIndexHintGolden(t *testing.T) {
	tests := []golden{
		{
			name:  "lock for update",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").Where("a", 1).LockForUpdate() },
			pg:    `SELECT * FROM "t" WHERE "a" = $1 FOR UPDATE`,
			my:    "SELECT * FROM `t` WHERE `a` = ? FOR UPDATE",
			lite:  `SELECT * FROM "t" WHERE "a" = ?`,
			args:  []any{1},
		},
		{
			name:  "shared lock",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").Limit(1).SharedLock() },
			pg:    `SELECT * FROM "t" LIMIT 1 FOR SHARE`,
			my:    "SELECT * FROM `t` LIMIT 1 LOCK IN SHARE MODE",
			lite:  `SELECT * FROM "t" LIMIT 1`,
		},
		{
			name:  "raw lock",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").Lock("FOR NO KEY UPDATE") },
			pg:    `SELECT * FROM "t" FOR NO KEY UPDATE`,
			my:    "SELECT * FROM `t` FOR NO KEY UPDATE",
			lite:  `SELECT * FROM "t"`,
		},
		{
			name: "use index",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").UseIndex("idx_a, idx_b").Where("a", 1)
			},
			pg:   `SELECT * FROM "t" WHERE "a" = $1`,
			my:   "SELECT * FROM `t` USE INDEX (idx_a, idx_b) WHERE `a` = ?",
			lite: `SELECT * FROM "t" WHERE "a" = ?`,
			args: []any{1},
		},
		{
			name:  "force index",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").ForceIndex("idx_a") },
			pg:    `SELECT * FROM "t"`,
			my:    "SELECT * FROM `t` FORCE INDEX (idx_a)",
			lite:  `SELECT * FROM "t" INDEXED BY idx_a`,
		},
		{
			name:  "ignore index",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").IgnoreIndex("idx_a") },
			pg:    `SELECT * FROM "t"`,
			my:    "SELECT * FROM `t` IGNORE INDEX (idx_a)",
			lite:  `SELECT * FROM "t"`,
		},
	}
	runGolden(t, tests)
}

func TestIndexHintErrors(t *testing.T) {
	tests := []struct {
		name  string
		build *db.Builder
		want  error
	}{
		{"index name injection", db.Use(db.MySQL).Table("t").UseIndex("a) ; drop table t; --"), db.ErrInvalidArgs},
		{"sqlite allows a single index", db.Use(db.SQLite).Table("t").ForceIndex("a, b"), db.ErrInvalidArgs},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build.ToSQL()
			assert.ErrorIs(t, err, tc.want)
		})
	}
}
