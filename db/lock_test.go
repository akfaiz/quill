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
			pg:    `select * from "t" where "a" = $1 for update`,
			my:    "select * from `t` where `a` = ? for update",
			lite:  `select * from "t" where "a" = ?`,
			args:  []any{1},
		},
		{
			name:  "shared lock",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").Limit(1).SharedLock() },
			pg:    `select * from "t" limit 1 for share`,
			my:    "select * from `t` limit 1 lock in share mode",
			lite:  `select * from "t" limit 1`,
		},
		{
			name:  "raw lock",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").Lock("for no key update") },
			pg:    `select * from "t" for no key update`,
			my:    "select * from `t` for no key update",
			lite:  `select * from "t"`,
		},
		{
			name: "use index",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").UseIndex("idx_a, idx_b").Where("a", 1)
			},
			pg:   `select * from "t" where "a" = $1`,
			my:   "select * from `t` use index (idx_a, idx_b) where `a` = ?",
			lite: `select * from "t" where "a" = ?`,
			args: []any{1},
		},
		{
			name:  "force index",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").ForceIndex("idx_a") },
			pg:    `select * from "t"`,
			my:    "select * from `t` force index (idx_a)",
			lite:  `select * from "t" indexed by idx_a`,
		},
		{
			name:  "ignore index",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").IgnoreIndex("idx_a") },
			pg:    `select * from "t"`,
			my:    "select * from `t` ignore index (idx_a)",
			lite:  `select * from "t"`,
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
