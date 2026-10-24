package cas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE TABLE IF NOT EXISTS applications (
 id text PRIMARY KEY, name text NOT NULL, key_hash text, status text NOT NULL DEFAULT 'ACTIVE', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS application_keys (
 id text PRIMARY KEY, application_id text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
 label text NOT NULL, environment text NOT NULL, key_prefix text NOT NULL, key_hash text NOT NULL UNIQUE,
 permissions jsonb NOT NULL DEFAULT '[]'::jsonb, expires_at timestamptz, last_used_at timestamptz,
 revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS application_keys_app_idx ON application_keys(application_id,created_at DESC);
INSERT INTO application_keys(id,application_id,label,environment,key_prefix,key_hash,permissions)
 SELECT 'legacy:'||id,id,'Legacy application key','CLIENT','legacy',key_hash,
 '["application.read","assets.read","assets.upload","assets.delete","references.read","references.write","grants.manage"]'::jsonb
 FROM applications WHERE key_hash IS NOT NULL ON CONFLICT(key_hash) DO NOTHING;
ALTER TABLE applications ALTER COLUMN key_hash DROP NOT NULL;
UPDATE applications SET key_hash=NULL WHERE key_hash IS NOT NULL;
CREATE TABLE IF NOT EXISTS assets (
 id text PRIMARY KEY, owner_application_id text NOT NULL REFERENCES applications(id), original_filename text NOT NULL,
 mime_type text NOT NULL, size_bytes bigint NOT NULL, checksum text NOT NULL, storage_key text NOT NULL,
 metadata jsonb NOT NULL DEFAULT '{}'::jsonb, status text NOT NULL DEFAULT 'ACTIVE', delete_requested_by text,
 delete_requested_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS assets_owner_idx ON assets(owner_application_id, created_at DESC);
CREATE TABLE IF NOT EXISTS asset_references (
 id text PRIMARY KEY, asset_id text NOT NULL REFERENCES assets(id), application_id text NOT NULL REFERENCES applications(id),
 entity_id text NOT NULL, active boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now(), deactivated_at timestamptz
);
CREATE INDEX IF NOT EXISTS refs_asset_idx ON asset_references(asset_id, active);
CREATE TABLE IF NOT EXISTS access_grants (
 id text PRIMARY KEY, asset_id text NOT NULL REFERENCES assets(id), application_id text NOT NULL REFERENCES applications(id),
 action text NOT NULL, granted_by text NOT NULL, active boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now(), revoked_at timestamptz,
 UNIQUE(asset_id, application_id, action)
);
CREATE TABLE IF NOT EXISTS audit_events (
 id text PRIMARY KEY, actor_application_id text NOT NULL, action text NOT NULL, asset_id text, result text NOT NULL,
 request_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_created_idx ON audit_events(created_at DESC);
CREATE TABLE IF NOT EXISTS cas_admins (
 user_id text PRIMARY KEY, role text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
DROP INDEX IF EXISTS cas_one_super_admin;
CREATE TABLE IF NOT EXISTS cas_local_users (
 user_id text PRIMARY KEY, username text NOT NULL, full_name text NOT NULL, role text,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL DEFAULT ''
);
INSERT INTO cas_local_users(user_id,username,full_name,role)
 SELECT user_id,'','',role FROM cas_admins WHERE role='SUPER_ADMIN'
 ON CONFLICT(user_id) DO NOTHING;
`

func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	for _, statement := range strings.Split(schema, ";") {
		if strings.TrimSpace(statement) == "" { continue }
		if _, err := db.Exec(ctx, statement); err != nil { return err }
	}
	return nil
}

func BootstrapApp(ctx context.Context, db *pgxpool.Pool, id, key string) error {
	id = strings.TrimSpace(id)
	if id == "" || len(key) < 32 { return errBootstrapKey }
	h := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(h[:])
	tag, err := db.Exec(ctx, `INSERT INTO applications(id,name,key_hash,status) VALUES($1,$1,$2,'ACTIVE') ON CONFLICT(id) DO NOTHING`, id, keyHash)
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return nil }
	_, err = db.Exec(ctx, `INSERT INTO application_keys(id,application_id,label,environment,key_prefix,key_hash,permissions)
		SELECT 'bootstrap:'||id,id,'Bootstrap key','CLIENT','legacy',$2,
		'["application.read","assets.read","assets.upload","assets.delete","references.read","references.write","grants.manage"]'::jsonb
		FROM applications WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM application_keys WHERE key_hash=$2)`, id, keyHash)
	if err != nil { return err }
	_, err = db.Exec(ctx, `UPDATE applications SET key_hash=NULL WHERE id=$1 AND key_hash=$2`, id, keyHash)
	return err
}
