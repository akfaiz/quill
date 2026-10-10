package db_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/akfaiz/quill/db"
)

func TestJoinGolden(t *testing.T) {
	tests := []golden{
		{
			name: "inner left right cross",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("users as u").
					Join("posts as p", "p.user_id", "u.id").
					LeftJoin("profiles", "profiles.user_id", "=", "u.id").
					RightJoin("teams", "teams.id", "<>", "u.team_id").
					CrossJoin("sizes").
					Where("u.active", true)
			},
			pg:   `select * from "users" as "u" inner join "posts" as "p" on "p"."user_id" = "u"."id" left join "profiles" on "profiles"."user_id" = "u"."id" right join "teams" on "teams"."id" <> "u"."team_id" cross join "sizes" where "u"."active" = $1`,
			my:   "select * from `users` as `u` inner join `posts` as `p` on `p`.`user_id` = `u`.`id` left join `profiles` on `profiles`.`user_id` = `u`.`id` right join `teams` on `teams`.`id` <> `u`.`team_id` cross join `sizes` where `u`.`active` = ?",
			lite: `select * from "users" as "u" inner join "posts" as "p" on "p"."user_id" = "u"."id" left join "profiles" on "profiles"."user_id" = "u"."id" right join "teams" on "teams"."id" <> "u"."team_id" cross join "sizes" where "u"."active" = ?`,
			args: []any{true},
		},
		{
			name: "complex on with values keeps arg order",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("users").
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
			pg:   `select * from "users" inner join "contacts" on "users"."id" = "contacts"."user_id" or "users"."alt_id" = "contacts"."user_id" and "contacts"."kind" = $1 and "contacts"."deleted_at" is null and ("contacts"."score" > $2 or "contacts"."tag" in ($3, $4)) where "a" = $5 and "b" = $6`,
			my:   "select * from `users` inner join `contacts` on `users`.`id` = `contacts`.`user_id` or `users`.`alt_id` = `contacts`.`user_id` and `contacts`.`kind` = ? and `contacts`.`deleted_at` is null and (`contacts`.`score` > ? or `contacts`.`tag` in (?, ?)) where `a` = ? and `b` = ?",
			lite: `select * from "users" inner join "contacts" on "users"."id" = "contacts"."user_id" or "users"."alt_id" = "contacts"."user_id" and "contacts"."kind" = ? and "contacts"."deleted_at" is null and ("contacts"."score" > ? or "contacts"."tag" in (?, ?)) where "a" = ? and "b" = ?`,
			args: []any{"primary", 5, "x", "y", 1, 2},
		},
		{
			name: "join where binds a value",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("u").JoinWhere("p", "p.status", "live").LeftJoinWhere("q", "q.n", ">", 3)
			},
			pg:   `select * from "u" inner join "p" on "p"."status" = $1 left join "q" on "q"."n" > $2`,
			my:   "select * from `u` inner join `p` on `p`.`status` = ? left join `q` on `q`.`n` > ?",
			lite: `select * from "u" inner join "p" on "p"."status" = ? left join "q" on "q"."n" > ?`,
			args: []any{"live", 3},
		},
		{
			name: "join subquery numbers args in order",
			build: func(d db.Dialect) *db.Builder {
				sub := db.Use(d).Table("posts").Select("user_id").SelectRaw("max(created_at) as last").Where("pub", true).GroupBy("user_id")
				return db.Use(d).Table("users").
					JoinSub(sub, "latest", "latest.user_id", "users.id").
					LeftJoinSubFunc(db.Use(d).Table("x").Where("k", 7), "xs", func(j *db.JoinClause) { j.On("xs.id", "users.id") }).
					Where("users.n", 1)
			},
			pg:   `select * from "users" inner join (select "user_id", max(created_at) as last from "posts" where "pub" = $1 group by "user_id") as "latest" on "latest"."user_id" = "users"."id" left join (select * from "x" where "k" = $2) as "xs" on "xs"."id" = "users"."id" where "users"."n" = $3`,
			my:   "select * from `users` inner join (select `user_id`, max(created_at) as last from `posts` where `pub` = ? group by `user_id`) as `latest` on `latest`.`user_id` = `users`.`id` left join (select * from `x` where `k` = ?) as `xs` on `xs`.`id` = `users`.`id` where `users`.`n` = ?",
			lite: `select * from "users" inner join (select "user_id", max(created_at) as last from "posts" where "pub" = ? group by "user_id") as "latest" on "latest"."user_id" = "users"."id" left join (select * from "x" where "k" = ?) as "xs" on "xs"."id" = "users"."id" where "users"."n" = ?`,
			args: []any{true, 7, 1},
		},
		{
			name: "prefix applies to join table and alias",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("users").Prefix("p_").Join("posts as po", "po.uid", "users.id")
			},
			pg:   `select * from "p_users" inner join "p_posts" as "p_po" on "p_po"."uid" = "p_users"."id"`,
			my:   "select * from `p_users` inner join `p_posts` as `p_po` on `p_po`.`uid` = `p_users`.`id`",
			lite: `select * from "p_users" inner join "p_posts" as "p_po" on "p_po"."uid" = "p_users"."id"`,
		},
	}
	runGolden(t, tests)
}

func TestJoinDialectSpecific(t *testing.T) {
	lateral := func(d db.Dialect) *db.Builder {
		sub := db.Use(d).Table("posts").Where("user_id", 1).Limit(3)
		return db.Use(d).Table("users").JoinLateral(sub, "recent").LeftJoinLateral(db.Use(d).Table("c"), "cc")
	}
	straight := func(d db.Dialect) *db.Builder {
		return db.Use(d).Table("a").StraightJoin("b", "b.id", "a.id")
	}

	got, args, err := lateral(db.Postgres).ToSQL()
	if err != nil {
		t.Fatal(err)
	}
	want := `select * from "users" inner join lateral (select * from "posts" where "user_id" = $1 limit 3) as "recent" on true left join lateral (select * from "c") as "cc" on true`
	if got != want || len(args) != 1 {
		t.Errorf("pg lateral\n got: %s %v\nwant: %s", got, args, want)
	}

	got, _, err = lateral(db.MySQL).ToSQL()
	if err != nil {
		t.Fatal(err)
	}
	want = "select * from `users` inner join lateral (select * from `posts` where `user_id` = ? limit 3) as `recent` on true left join lateral (select * from `c`) as `cc` on true"
	if got != want {
		t.Errorf("mysql lateral\n got: %s\nwant: %s", got, want)
	}

	got, _, err = straight(db.MySQL).ToSQL()
	if err != nil {
		t.Fatal(err)
	}
	if want = "select * from `a` straight_join `b` on `b`.`id` = `a`.`id`"; got != want {
		t.Errorf("mysql straight\n got: %s\nwant: %s", got, want)
	}

	for _, d := range []db.Dialect{db.Postgres, db.SQLite} {
		if _, _, err := straight(d).ToSQL(); !errors.Is(err, db.ErrUnsupported) {
			t.Errorf("%s straight_join: got %v want ErrUnsupported", d, err)
		}
	}
	// MariaDB: straight_join works as on MySQL, lateral joins do not exist
	got, _, err = straight(db.MariaDB).ToSQL()
	if err != nil || got != "select * from `a` straight_join `b` on `b`.`id` = `a`.`id`" {
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
		build *db.Builder
		want  error
	}{
		{"missing on", db.Use(d).Table("a").Join("b", ""), db.ErrJoinNeedsOn},
		{"empty func", db.Use(d).Table("a").JoinFunc("b", func(*db.JoinClause) {}), db.ErrJoinNeedsOn},
		{"bad operator", db.Use(d).Table("a").Join("b", "b.x", "bogus", "a.x"), db.ErrInvalidOperator},
		{"second column not string", db.Use(d).Table("a").Join("b", "b.x", 5), db.ErrInvalidArgs},
		{"sub needs alias", db.Use(d).Table("a").JoinSub(db.Use(d).Table("s"), "", "s.x", "a.x"), db.ErrInvalidArgs},
		{"bad sub propagates", db.Use(d).Table("a").JoinSub(db.NewBuilder(d), "s", "s.x", "a.x"), db.ErrNoTable},
		{"error inside on func", db.Use(d).Table("a").JoinFunc("b", func(j *db.JoinClause) { j.Where("x", "bogus", 1) }), db.ErrInvalidOperator},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := tc.build.ToSQL(); !errors.Is(err, tc.want) {
				t.Errorf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestJoinSubIsSnapshotAndCloneIndependent(t *testing.T) {
	sub := db.Use(db.Postgres).Table("s").Where("a", 1)
	base := db.Use(db.Postgres).Table("t").JoinSub(sub, "x", "x.id", "t.id")
	c := base.Clone().LeftJoin("z", "z.id", "t.id")
	sub.Where("b", 2)
	bs, _, _ := base.ToSQL()
	cs, _, _ := c.ToSQL()
	if want := `select * from "t" inner join (select * from "s" where "a" = $1) as "x" on "x"."id" = "t"."id"`; bs != want {
		t.Errorf("base:\n got: %s\nwant: %s", bs, want)
	}
	if want := `select * from "t" inner join (select * from "s" where "a" = $1) as "x" on "x"."id" = "t"."id" left join "z" on "z"."id" = "t"."id"`; cs != want {
		t.Errorf("clone:\n got: %s\nwant: %s", cs, want)
	}
}

// TestJoinVariantsPostgres pins the join helpers the golden tables do not reach:
// the remaining JoinClause conditions, right joins and every subquery-join form.
func TestJoinVariantsPostgres(t *testing.T) {
	d := db.Postgres
	tb := func() *db.Builder { return db.Use(d).Table("t") }
	sub := func() *db.Builder { return db.Use(d).Table("s").Where("k", 1) }
	tests := []struct {
		name  string
		build *db.Builder
		sql   string
		args  []any
	}{
		{"every join clause condition", tb().JoinFunc("j", func(j *db.JoinClause) {
			j.On("j.a", "t.a").
				OrOnGroup(func(g *db.JoinClause) { g.On("j.b", "t.b").OrOn("j.c", "t.c") }).
				WhereIn("j.k", []int{1}).WhereNotIn("j.k", []int{2}).
				OrWhere("j.z", ">", 3).OrWhereIn("j.y", []int{4}).OrWhereNotIn("j.y", []int{5}).
				WhereNotNull("j.n").OrWhereNull("j.m").OrWhereNotNull("j.o").
				WhereRaw("j.r = ?", 6).OrWhereRaw("j.q = ?", 7)
		}),
			`select * from "t" inner join "j" on "j"."a" = "t"."a" or ("j"."b" = "t"."b" or "j"."c" = "t"."c") and "j"."k" in ($1) and "j"."k" not in ($2) or "j"."z" > $3 or "j"."y" in ($4) or "j"."y" not in ($5) and "j"."n" is not null or "j"."m" is null or "j"."o" is not null and j.r = $6 or j.q = $7`,
			[]any{1, 2, 3, 4, 5, 6, 7}},
		{"right join func and where", tb().RightJoinFunc("r", func(j *db.JoinClause) { j.On("r.a", "t.a") }).RightJoinWhere("w", "w.x", 9),
			`select * from "t" right join "r" on "r"."a" = "t"."a" right join "w" on "w"."x" = $1`, []any{9}},
		{"every subquery join form", tb().
			LeftJoinSub(sub(), "l", "l.a", "t.a").
			RightJoinSub(sub(), "r", "r.a", "t.a").
			CrossJoinSub(sub(), "c").
			JoinSubFunc(sub(), "f", func(j *db.JoinClause) { j.On("f.a", "t.a") }).
			RightJoinSubFunc(sub(), "g", func(j *db.JoinClause) { j.On("g.a", "t.a") }),
			`select * from "t" left join (select * from "s" where "k" = $1) as "l" on "l"."a" = "t"."a" right join (select * from "s" where "k" = $2) as "r" on "r"."a" = "t"."a" cross join (select * from "s" where "k" = $3) as "c" inner join (select * from "s" where "k" = $4) as "f" on "f"."a" = "t"."a" right join (select * from "s" where "k" = $5) as "g" on "g"."a" = "t"."a"`,
			[]any{1, 1, 1, 1, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, err := tc.build.ToSQL()
			if err != nil {
				t.Fatal(err)
			}
			if sql != tc.sql {
				t.Errorf("sql\n got: %s\nwant: %s", sql, tc.sql)
			}
			if !reflect.DeepEqual(args, tc.args) {
				t.Errorf("args got %v want %v", args, tc.args)
			}
		})
	}
}
