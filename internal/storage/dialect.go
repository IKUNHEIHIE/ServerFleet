package storage

import (
	"regexp"
	"strings"
)

type dialect string

const (
	dialectSQLite   dialect = "sqlite"
	dialectPostgres dialect = "postgres"
)

var (
	jsonExtractPattern = regexp.MustCompile(`json_extract\((.+?),'\$\.([^']+)'\)`)
	insertIgnorePrefix = regexp.MustCompile(`(?i)^\s*INSERT\s+OR\s+IGNORE\s+INTO\s+`)
	triggerUpdate      = regexp.MustCompile(`(?is)^\s*CREATE\s+TRIGGER\s+([A-Za-z0-9_]+)\s+BEFORE\s+UPDATE\s+ON\s+([A-Za-z0-9_]+).*`)
	booleanSum         = regexp.MustCompile(`\(([^()]*) <> ''\) \+ \(([^()]*) <> ''\)`)
	sqliteMasterCount  = regexp.MustCompile(`(?i)^\s*SELECT\s+count\(\*\)\s+FROM\s+sqlite_master\s+WHERE\s+type\s*=\s*'table'\s+AND\s+name\s*=\s*'([^']+)'`)
	sqliteMasterName   = regexp.MustCompile(`(?i)^\s*SELECT\s+name\s+FROM\s+sqlite_master\s+WHERE\s+type\s*=\s*'table'\s+AND\s+name\s*=\s*'([^']+)'`)
	sqliteMasterAll    = regexp.MustCompile(`(?is)^\s*SELECT\s+type,name,tbl_name,COALESCE\(sql,''\)\s+FROM\s+sqlite_master.*`)
)

// prepare rewrites SQLite-authored SQL for the active driver. Business modules
// keep one statement vocabulary; the storage layer owns placeholder and the
// small set of dialect differences used by this repository.
func (d dialect) prepare(query string) string {
	if d != dialectPostgres {
		return query
	}
	if match := sqliteMasterCount.FindStringSubmatch(query); len(match) == 2 {
		return "SELECT count(*) FROM information_schema.tables WHERE table_name = " + quoteLiteral(match[1])
	}
	if match := sqliteMasterName.FindStringSubmatch(query); len(match) == 2 {
		return "SELECT table_name FROM information_schema.tables WHERE table_name = " + quoteLiteral(match[1])
	}
	if sqliteMasterAll.MatchString(query) {
		return "SELECT 'table' AS type, table_name AS name, table_name AS tbl_name, '' AS sql FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name"
	}
	if match := triggerUpdate.FindStringSubmatch(query); len(match) == 3 {
		return "CREATE RULE " + match[1] + " AS ON UPDATE TO " + match[2] + " DO INSTEAD NOTHING"
	}
	if strings.HasPrefix(strings.TrimSpace(query), "PRAGMA table_info(") {
		return `SELECT ordinal_position AS cid, column_name AS name, data_type AS type,
			CASE WHEN is_nullable = 'NO' THEN 1 ELSE 0 END AS notnull,
			NULL::text AS dflt_value, 0 AS pk
			FROM information_schema.columns WHERE table_name = ` + quoteLiteral(tableInfoName(query)) + ` ORDER BY ordinal_position`
	}
	if strings.EqualFold(strings.TrimSpace(query), "PRAGMA user_version") {
		return "SELECT 0"
	}
	q := query
	q = strings.ReplaceAll(q, "INTEGER PRIMARY KEY AUTOINCREMENT", "BIGSERIAL PRIMARY KEY")
	q = strings.ReplaceAll(q, "BLOB", "BYTEA")
	q = strings.ReplaceAll(q, "COLLATE NOCASE", "")
	q = strings.ReplaceAll(q, "length(payload)<=16777216", "octet_length(payload)<=16777216")
	q = booleanSum.ReplaceAllString(q, `(CASE WHEN $1 <> '' THEN 1 ELSE 0 END) + (CASE WHEN $2 <> '' THEN 1 ELSE 0 END)`)
	q = jsonExtractPattern.ReplaceAllStringFunc(q, func(match string) string {
		parts := jsonExtractPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		path := "{" + strings.ReplaceAll(parts[2], ".", ",") + "}"
		return "(" + parts[1] + "::jsonb #>> '" + path + "')"
	})
	if insertIgnorePrefix.MatchString(q) {
		q = insertIgnorePrefix.ReplaceAllString(q, "INSERT INTO ")
		if !strings.Contains(strings.ToUpper(q), "ON CONFLICT") {
			q = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(q), ";")) + " ON CONFLICT DO NOTHING"
		}
	}
	return rewritePlaceholders(q)
}

func tableInfoName(query string) string {
	start := strings.Index(query, "(")
	end := strings.LastIndex(query, ")")
	if start < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(query[start+1 : end])
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// rewritePlaceholders converts ? placeholders to $1..$n outside quoted strings.
func rewritePlaceholders(query string) string {
	var out strings.Builder
	out.Grow(len(query) + 16)
	index := 0
	single, double := false, false
	for _, char := range query {
		switch {
		case char == '\'' && !double:
			single = !single
			out.WriteRune(char)
		case char == '"' && !single:
			double = !double
			out.WriteRune(char)
		case char == '?' && !single && !double:
			index++
			out.WriteString("$")
			out.WriteString(itoa(index))
		default:
			out.WriteRune(char)
		}
	}
	return out.String()
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}