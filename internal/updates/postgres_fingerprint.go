package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/storage"
)

// Unlike the old sqlite_master compatibility rewrite this includes columns,
// defaults, constraints, indexes, views, rules, triggers and sequence definitions.
// Application rows and sequence counters deliberately do not affect the hash.
func postgresSchemaFingerprint(ctx context.Context, db *storage.DB) (string, error) {
	rows, err := db.Query(ctx, postgresSchemaSQL)
	if err != nil {
		return "", ErrArtifactRejected
	}
	defer rows.Close()
	state := struct {
		Driver  string
		Entries [][4]string
	}{Driver: "postgres-schema-v1"}
	for rows.Next() {
		var item [4]string
		if err = rows.Scan(&item[0], &item[1], &item[2], &item[3]); err != nil {
			return "", ErrArtifactRejected
		}
		if len(state.Entries) >= 16384 {
			return "", ErrArtifactRejected
		}
		state.Entries = append(state.Entries, item)
	}
	if rows.Err() != nil {
		return "", ErrArtifactRejected
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", ErrArtifactRejected
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

const postgresSchemaSQL = "SELECT kind,name,relation,definition FROM (\nSELECT 'column'::text kind,c.relname||'.'||a.attname name,c.relname relation,\n jsonb_build_array(a.attnum,pg_catalog.format_type(a.atttypid,a.atttypmod),a.attnotnull,\n pg_catalog.pg_get_expr(d.adbin,d.adrelid),a.attidentity,a.attgenerated,co.collname)::text definition\n FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace\n JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped\n LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum\n LEFT JOIN pg_catalog.pg_collation co ON co.oid=a.attcollation WHERE n.nspname='public'\nUNION ALL SELECT 'relation',c.relname,c.relname,\n jsonb_build_array(c.relkind,c.relpersistence,c.relrowsecurity,c.relforcerowsecurity,c.reloptions,\n CASE WHEN c.relkind IN ('v','m') THEN pg_catalog.pg_get_viewdef(c.oid) ELSE '' END)::text\n FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'\nUNION ALL SELECT 'constraint',c.relname||'.'||k.conname,c.relname,pg_catalog.pg_get_constraintdef(k.oid)||':'||k.convalidated::text\n FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_class c ON c.oid=k.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'\nUNION ALL SELECT 'index',i.relname,c.relname,pg_catalog.pg_get_indexdef(x.indexrelid)\n FROM pg_catalog.pg_index x JOIN pg_catalog.pg_class i ON i.oid=x.indexrelid JOIN pg_catalog.pg_class c ON c.oid=x.indrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'\nUNION ALL SELECT 'trigger',c.relname||'.'||t.tgname,c.relname,pg_catalog.pg_get_triggerdef(t.oid)||':'||t.tgenabled::text\n FROM pg_catalog.pg_trigger t JOIN pg_catalog.pg_class c ON c.oid=t.tgrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND NOT t.tgisinternal\nUNION ALL SELECT 'rule',c.relname||'.'||r.rulename,c.relname,pg_catalog.pg_get_ruledef(r.oid)||':'||r.ev_enabled::text\n FROM pg_catalog.pg_rewrite r JOIN pg_catalog.pg_class c ON c.oid=r.ev_class JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'\nUNION ALL SELECT 'sequence',c.relname,c.relname,jsonb_build_array(s.seqstart,s.seqincrement,s.seqmax,s.seqmin,s.seqcache,s.seqcycle)::text\n FROM pg_catalog.pg_sequence s JOIN pg_catalog.pg_class c ON c.oid=s.seqrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'\nUNION ALL SELECT 'function',p.oid::regprocedure::text,'',pg_catalog.pg_get_functiondef(p.oid)\n FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.prokind IN ('f','p')\nUNION ALL SELECT 'policy',c.relname||'.'||p.polname,c.relname,\n jsonb_build_array(p.polcmd,p.polpermissive,p.polroles,pg_catalog.pg_get_expr(p.polqual,p.polrelid),pg_catalog.pg_get_expr(p.polwithcheck,p.polrelid))::text\n FROM pg_catalog.pg_policy p JOIN pg_catalog.pg_class c ON c.oid=p.polrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'\n) entries ORDER BY kind,name,relation,definition"
