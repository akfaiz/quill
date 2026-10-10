package dialect

// MariaDB returns the MariaDB grammar. It is MySQL's with one difference at this
// level: MariaDB has no LATERAL joins. The other difference, json_query instead of
// CAST AS JSON for JSON container values, is compiled in the root package.
func MariaDB() Grammar { return mariadbGrammar{} }

type mariadbGrammar struct{ mysqlGrammar }

// SupportsLateralJoin: MariaDB does not implement LATERAL.
func (mariadbGrammar) SupportsLateralJoin() bool { return false }
