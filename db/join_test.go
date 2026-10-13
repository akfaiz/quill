package db_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"

	"github.com/akfaiz/quill/db"
)

func TestJoinGolden(t *testing.T) {
	tests := []golden{
		{
			name: "inner left right cross",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).Select().From("users as u").
					Join("posts as p", "p.user_id", "u.id").
					LeftJoin("profiles", "profiles.user_id", "=", "u.id").
					RightJoin("teams", "teams.id", "<>", "u.team_id").
					CrossJoin("sizes").
					Where("u.active", true)
			},
			pg:   `SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" LEFT JOIN "profiles" ON "profiles"."user_id" = "u"."id" RIGHT JOIN "teams" ON "teams"."id" <> "u"."team_id" CROSS JOIN "sizes" WHERE "u"."active" = $1`,
			my:   "SELECT * FROM `users` AS `u` INNER JOIN `posts` AS `p` ON `p`.`user_id` = `u`.`id` LEFT JOIN `profiles` ON `profiles`.`user_id` = `u`.`id` RIGHT JOIN `teams` ON `teams`.`id` <> `u`.`team_id` CROSS JOIN `sizes` WHERE `u`.`active` = ?",
			lite: `SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" LEFT JOIN "profiles" ON "profiles"."user_id" = "u"."id" RIGHT JOIN "teams" ON "teams"."id" <> "u"."team_id" CROSS JOIN "sizes" WHERE "u"."active" = ?`,
			args: []any{true},
		},
		{
			name: "complex on with values keeps arg order",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).Select().From("users").
					Where("a", 1).
					JoinFunc("contacts", func(j *db.JoinClause) {
						j.On("users.id", "contacts.user_id").
							OrOn("users.alt_id", "contacts.user_id").
							Where("contacts.kind", "primary").
							WhereNull("contacts.deleted_at").
							OnGroup(func(g *db.JoinClause) {
								g.Where("contacts.score", ">", 5).OrWhereIn("contacts.tag", []string{"x", "y"})
							})
					}).
					Where("b", 2)
			},
			pg:   `SELECT * FROM "users" INNER JOIN "contacts" ON "users"."id" = "contacts"."user_id" OR "users"."alt_id" = "contacts"."user_id" AND "contacts"."kind" = $1 AND "contacts"."deleted_at" IS NULL AND ("contacts"."score" > $2 OR "contacts"."tag" IN ($3, $4)) WHERE "a" = $5 AND "b" = $6`,
			my:   "SELECT * FROM `users` INNER JOIN `contacts` ON `users`.`id` = `contacts`.`user_id` OR `users`.`alt_id` = `contacts`.`user_id` AND `contacts`.`kind` = ? AND `contacts`.`deleted_at` IS NULL AND (`contacts`.`score` > ? OR `contacts`.`tag` IN (?, ?)) WHERE `a` = ? AND `b` = ?",
			lite: `SELECT * FROM "users" INNER JOIN "contacts" ON "users"."id" = "contacts"."user_id" OR "users"."alt_id" = "contacts"."user_id" AND "contacts"."kind" = ? AND "contacts"."deleted_at" IS NULL AND ("contacts"."score" > ? OR "contacts"."tag" IN (?, ?)) WHERE "a" = ? AND "b" = ?`,
			args: []any{"primary", 5, "x", "y", 1, 2},
		},
		{
			name: "join where binds a value",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).Select().From("u").JoinWhere("p", "p.status", "live").LeftJoinWhere("q", "q.n", ">", 3)
			},
			pg:   `SELECT * FROM "u" INNER JOIN "p" ON "p"."status" = $1 LEFT JOIN "q" ON "q"."n" > $2`,
			my:   "SELECT * FROM `u` INNER JOIN `p` ON `p`.`status` = ? LEFT JOIN `q` ON `q`.`n` > ?",
			lite: `SELECT * FROM "u" INNER JOIN "p" ON "p"."status" = ? LEFT JOIN "q" ON "q"."n" > ?`,
			args: []any{"live", 3},
		},
		{
			name: "join subquery numbers args in order",
			build: func(d db.Dialect) *db.SelectBuilder {
				sub := db.Use(d).
					Select().From("posts").
					Select("user_id").
					SelectRaw("max(created_at) as last").
					Where("pub", true).
					GroupBy("user_id")
				return db.Use(d).Select().From("users").
					JoinSub(sub, "latest", "latest.user_id", "users.id").
					LeftJoinSubFunc(db.Use(d).Select().From("x").Where("k", 7), "xs", func(j *db.JoinClause) { j.On("xs.id", "users.id") }).
					Where("users.n", 1)
			},
			pg:   `SELECT * FROM "users" INNER JOIN (SELECT "user_id", max(created_at) as last FROM "posts" WHERE "pub" = $1 GROUP BY "user_id") AS "latest" ON "latest"."user_id" = "users"."id" LEFT JOIN (SELECT * FROM "x" WHERE "k" = $2) AS "xs" ON "xs"."id" = "users"."id" WHERE "users"."n" = $3`,
			my:   "SELECT * FROM `users` INNER JOIN (SELECT `user_id`, max(created_at) as last FROM `posts` WHERE `pub` = ? GROUP BY `user_id`) AS `latest` ON `latest`.`user_id` = `users`.`id` LEFT JOIN (SELECT * FROM `x` WHERE `k` = ?) AS `xs` ON `xs`.`id` = `users`.`id` WHERE `users`.`n` = ?",
			lite: `SELECT * FROM "users" INNER JOIN (SELECT "user_id", max(created_at) as last FROM "posts" WHERE "pub" = ? GROUP BY "user_id") AS "latest" ON "latest"."user_id" = "users"."id" LEFT JOIN (SELECT * FROM "x" WHERE "k" = ?) AS "xs" ON "xs"."id" = "users"."id" WHERE "users"."n" = ?`,
			args: []any{true, 7, 1},
		},
		{
			name: "prefix applies to join table and alias",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).Select().From("users").Prefix("p_").Join("posts as po", "po.uid", "users.id")
			},
			pg:   `SELECT * FROM "p_users" INNER JOIN "p_posts" AS "p_po" ON "p_po"."uid" = "p_users"."id"`,
			my:   "SELECT * FROM `p_users` INNER JOIN `p_posts` AS `p_po` ON `p_po`.`uid` = `p_users`.`id`",
			lite: `SELECT * FROM "p_users" INNER JOIN "p_posts" AS "p_po" ON "p_po"."uid" = "p_users"."id"`,
		},
	}
	runGolden(t, tests)
}

func TestJoinDialectSpecific(t *testing.T) {
	lateral := func(d db.Dialect) *db.SelectBuilder {
		sub := db.Use(d).Select().From("posts").Where("user_id", 1).Limit(3)
		return db.Use(d).
			Select().
			From("users").
			JoinLateral(sub, "recent").
			LeftJoinLateral(db.Use(d).Select().From("c"), "cc")
	}
	straight := func(d db.Dialect) *db.SelectBuilder {
		return db.Use(d).Select().From("a").StraightJoin("b", "b.id", "a.id")
	}

	got, args, err := lateral(db.Postgres).ToSQL()
	require.NoError(t, err)
	want := `SELECT * FROM "users" INNER JOIN LATERAL (SELECT * FROM "posts" WHERE "user_id" = $1 LIMIT 3) AS "recent" ON TRUE LEFT JOIN LATERAL (SELECT * FROM "c") AS "cc" ON TRUE`
	if got != want || len(args) != 1 {
		t.Errorf("pg lateral\n got: %s %v\nwant: %s", got, args, want)
	}

	got, _, err = lateral(db.MySQL).ToSQL()
	require.NoError(t, err)
	want = "SELECT * FROM `users` INNER JOIN LATERAL (SELECT * FROM `posts` WHERE `user_id` = ? LIMIT 3) AS `recent` ON TRUE LEFT JOIN LATERAL (SELECT * FROM `c`) AS `cc` ON TRUE"
	assert.Equal(t, want, got)

	got, _, err = straight(db.MySQL).ToSQL()
	require.NoError(t, err)
	if want = "SELECT * FROM `a` STRAIGHT_JOIN `b` ON `b`.`id` = `a`.`id`"; got != want {
		t.Errorf("mysql straight\n got: %s\nwant: %s", got, want)
	}

	for _, d := range []db.Dialect{db.Postgres, db.SQLite} {
		if _, _, sErr := straight(d).ToSQL(); !errors.Is(sErr, db.ErrUnsupported) {
			t.Errorf("%s straight_join: got %v want ErrUnsupported", d, sErr)
		}
	}
	// MariaDB: straight_join works as on MySQL, lateral joins do not exist
	got, _, err = straight(db.MariaDB).ToSQL()
	if err != nil || got != "SELECT * FROM `a` STRAIGHT_JOIN `b` ON `b`.`id` = `a`.`id`" {
		t.Errorf("mariadb straight: %s (%v)", got, err)
	}
	if _, _, err := lateral(db.MariaDB).ToSQL(); !errors.Is(err, db.ErrUnsupported) {
		t.Errorf("mariadb lateral: got %v want ErrUnsupported", err)
	}
	if _, _, err := lateral(db.SQLite).ToSQL(); !errors.Is(err, db.ErrUnsupported) {
		t.Errorf("sqlite lateral: got %v want ErrUnsupported", err)
	}
}

func TestJoinErrors(t *testing.T) {
	d := db.Postgres
	tests := []struct {
		name  string
		build *db.SelectBuilder
		want  error
	}{
		{"missing on", db.Use(d).Select().From("a").Join("b", ""), db.ErrJoinNeedsOn},
		{"empty func", db.Use(d).Select().From("a").JoinFunc("b", func(*db.JoinClause) {}), db.ErrJoinNeedsOn},
		{"bad operator", db.Use(d).Select().From("a").Join("b", "b.x", "bogus", "a.x"), db.ErrInvalidOperator},
		{"second column not string", db.Use(d).Select().From("a").Join("b", "b.x", 5), db.ErrInvalidArgs},
		{
			"sub needs alias",
			db.Use(d).Select().From("a").JoinSub(db.Use(d).Select().From("s"), "", "s.x", "a.x"),
			db.ErrInvalidArgs,
		},
		{
			"bad sub propagates",
			db.Use(d).Select().From("a").JoinSub(db.Use(d).Select(), "s", "s.x", "a.x"),
			db.ErrNoTable,
		},
		{"nil sub", db.Use(d).Select().From("a").JoinSub(nil, "s", "s.x", "a.x"), db.ErrInvalidArgs},
		{"nil cross sub", db.Use(d).Select().From("a").CrossJoinSub(nil, "s"), db.ErrInvalidArgs},
		{"empty table", db.Use(d).Select().From("a").Join("", "s.x", "a.x"), db.ErrInvalidArgs},
		{
			"error inside on func",
			db.Use(d).Select().From("a").JoinFunc("b", func(j *db.JoinClause) { j.Where("x", "bogus", 1) }),
			db.ErrInvalidOperator,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build.ToSQL()
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestJoinSubIsSnapshotAndCloneIndependent(t *testing.T) {
	sub := db.Use(db.Postgres).Select().From("s").Where("a", 1)
	base := db.Use(db.Postgres).Select().From("t").JoinSub(sub, "x", "x.id", "t.id")
	c := base.Clone().LeftJoin("z", "z.id", "t.id")
	sub.Where("b", 2)
	bs, _, _ := base.ToSQL()
	cs, _, _ := c.ToSQL()
	if want := `SELECT * FROM "t" INNER JOIN (SELECT * FROM "s" WHERE "a" = $1) AS "x" ON "x"."id" = "t"."id"`; bs != want {
		t.Errorf("base:\n got: %s\nwant: %s", bs, want)
	}
	if want := `SELECT * FROM "t" INNER JOIN (SELECT * FROM "s" WHERE "a" = $1) AS "x" ON "x"."id" = "t"."id" LEFT JOIN "z" ON "z"."id" = "t"."id"`; cs != want {
		t.Errorf("clone:\n got: %s\nwant: %s", cs, want)
	}
}

// TestJoinVariantsPostgres pins the join helpers the golden tables do not reach:
// the remaining JoinClause conditions, right joins and every subquery-join form.
func TestJoinVariantsPostgres(t *testing.T) {
	d := db.Postgres
	tb := func() *db.SelectBuilder { return db.Use(d).Select().From("t") }
	sub := func() *db.SelectBuilder { return db.Use(d).Select().From("s").Where("k", 1) }
	tests := []struct {
		name  string
		build *db.SelectBuilder
		sql   string
		args  []any
	}{
		{
			"every join clause condition",
			tb().JoinFunc("j", func(j *db.JoinClause) {
				j.On("j.a", "t.a").
					OrOnGroup(func(g *db.JoinClause) { g.On("j.b", "t.b").OrOn("j.c", "t.c") }).
					WhereIn("j.k", []int{1}).WhereNotIn("j.k", []int{2}).
					OrWhere("j.z", ">", 3).OrWhereIn("j.y", []int{4}).OrWhereNotIn("j.y", []int{5}).
					WhereNotNull("j.n").OrWhereNull("j.m").OrWhereNotNull("j.o").
					WhereRaw("j.r = ?", 6).OrWhereRaw("j.q = ?", 7)
			}),
			`SELECT * FROM "t" INNER JOIN "j" ON "j"."a" = "t"."a" OR ("j"."b" = "t"."b" OR "j"."c" = "t"."c") AND "j"."k" IN ($1) AND "j"."k" NOT IN ($2) OR "j"."z" > $3 OR "j"."y" IN ($4) OR "j"."y" NOT IN ($5) AND "j"."n" IS NOT NULL OR "j"."m" IS NULL OR "j"."o" IS NOT NULL AND j.r = $6 OR j.q = $7`,
			[]any{1, 2, 3, 4, 5, 6, 7},
		},
		{
			"right join func and where",
			tb().RightJoinFunc("r", func(j *db.JoinClause) { j.On("r.a", "t.a") }).RightJoinWhere("w", "w.x", 9),
			`SELECT * FROM "t" RIGHT JOIN "r" ON "r"."a" = "t"."a" RIGHT JOIN "w" ON "w"."x" = $1`,
			[]any{9},
		},
		{
			"every subquery join form",
			tb().
				LeftJoinSub(sub(), "l", "l.a", "t.a").
				RightJoinSub(sub(), "r", "r.a", "t.a").
				CrossJoinSub(sub(), "c").
				JoinSubFunc(sub(), "f", func(j *db.JoinClause) { j.On("f.a", "t.a") }).
				RightJoinSubFunc(sub(), "g", func(j *db.JoinClause) { j.On("g.a", "t.a") }),
			`SELECT * FROM "t" LEFT JOIN (SELECT * FROM "s" WHERE "k" = $1) AS "l" ON "l"."a" = "t"."a" RIGHT JOIN (SELECT * FROM "s" WHERE "k" = $2) AS "r" ON "r"."a" = "t"."a" CROSS JOIN (SELECT * FROM "s" WHERE "k" = $3) AS "c" INNER JOIN (SELECT * FROM "s" WHERE "k" = $4) AS "f" ON "f"."a" = "t"."a" RIGHT JOIN (SELECT * FROM "s" WHERE "k" = $5) AS "g" ON "g"."a" = "t"."a"`,
			[]any{1, 1, 1, 1, 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, err := tc.build.ToSQL()
			require.NoError(t, err)
			assert.Equal(t, tc.sql, sql)
			assert.Equal(t, tc.args, args)
		})
	}
}
