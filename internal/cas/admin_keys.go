package cas

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

var applicationKeyPermissions = map[string]bool{
	"application.read": true,
	"assets.read": true,
	"assets.upload": true,
	"assets.delete": true,
	"references.read": true,
	"references.write": true,
	"grants.manage": true,
}

func (s *Service) adminApplicationKeys(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/applications/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "keys" {
		fail(w, http.StatusNotFound, "application keys not found")
		return
	}
	appID := parts[0]
	if len(parts) == 2 && r.Method == http.MethodGet {
		rows, err := s.db.Query(r.Context(), `SELECT k.id,k.label,k.environment,k.key_prefix,k.permissions,k.expires_at,k.last_used_at,k.revoked_at,k.created_at,a.status
			FROM application_keys k JOIN applications a ON a.id=k.application_id WHERE k.application_id=$1 ORDER BY k.created_at DESC`, appID)
		if err != nil {
			fail(w, http.StatusInternalServerError, "application keys unavailable")
			return
		}
		defer rows.Close()
		keys := []map[string]any{}
		for rows.Next() {
			var id, label, environment, prefix string
			var rawPermissions []byte
			var expiresAt, lastUsedAt, revokedAt *time.Time
			var createdAt time.Time
			var appStatus string
			if err := rows.Scan(&id, &label, &environment, &prefix, &rawPermissions, &expiresAt, &lastUsedAt, &revokedAt, &createdAt, &appStatus); err != nil {
				fail(w, http.StatusInternalServerError, "application keys unavailable")
				return
			}
			var permissions []string
			if json.Unmarshal(rawPermissions, &permissions) != nil {
				permissions = []string{}
			}
			active := appStatus == "ACTIVE" && revokedAt == nil && (expiresAt == nil || expiresAt.After(time.Now()))
			keys = append(keys, map[string]any{"key_id": id, "label": label, "environment": environment, "key_prefix": prefix,
				"permissions": permissions, "expires_at": expiresAt, "last_used_at": lastUsedAt, "revoked_at": revokedAt,
				"created_at": createdAt, "active": active})
		}
		if rows.Err() != nil {
			fail(w, http.StatusInternalServerError, "application keys unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": keys})
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		var input struct {
			Label       string   `json:"label"`
			Environment string   `json:"environment"`
			Permissions []string `json:"permissions"`
			DurationDays *int    `json:"duration_days"`
		}
		if decodeJSON(r, &input) != nil {
			fail(w, http.StatusUnprocessableEntity, "invalid key request")
			return
		}
		input.Label = strings.TrimSpace(input.Label)
		input.Environment = strings.ToUpper(strings.TrimSpace(input.Environment))
		if input.Label == "" || len(input.Label) > 100 {
			fail(w, http.StatusUnprocessableEntity, "label is required and must be at most 100 characters")
			return
		}
		if input.Environment != "DEV" && input.Environment != "TEST" && input.Environment != "PRODUCTION" {
			fail(w, http.StatusUnprocessableEntity, "environment must be DEV, TEST, or PRODUCTION")
			return
		}
		if input.DurationDays == nil {
			fail(w, http.StatusUnprocessableEntity, "duration_days is required; use 0 for no expiry")
			return
		}
		durationDays := *input.DurationDays
		if durationDays != 7 && durationDays != 30 && durationDays != 90 && durationDays != 180 && durationDays != 365 && durationDays != 0 {
			fail(w, http.StatusUnprocessableEntity, "duration_days must be 7, 30, 90, 180, 365, or 0 for no expiry")
			return
		}
		if len(input.Permissions) == 0 || len(input.Permissions) > len(applicationKeyPermissions) {
			fail(w, http.StatusUnprocessableEntity, "select at least one supported permission")
			return
		}
		seen := map[string]bool{}
		for _, permission := range input.Permissions {
			if !applicationKeyPermissions[permission] || seen[permission] {
				fail(w, http.StatusUnprocessableEntity, "permissions contains an unsupported or duplicate value")
				return
			}
			seen[permission] = true
		}
		var appStatus string
		if err := s.db.QueryRow(r.Context(), `SELECT status FROM applications WHERE id=$1`, appID).Scan(&appStatus); err != nil {
			fail(w, http.StatusNotFound, "application not found")
			return
		}
		if appStatus != "ACTIVE" { fail(w, http.StatusConflict, "application must be ACTIVE to create a key"); return }
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			fail(w, http.StatusInternalServerError, "application key could not be generated")
			return
		}
		secret := "cas_" + strings.ToLower(input.Environment) + "_" + base64.RawURLEncoding.EncodeToString(secretBytes)
		hash := sha256.Sum256([]byte(secret))
		permissions, _ := json.Marshal(input.Permissions)
		var expiresAt *time.Time
		if durationDays > 0 {
			expires := time.Now().UTC().Add(time.Duration(durationDays) * 24 * time.Hour)
			expiresAt = &expires
		}
		id := newID()
		prefix := secret
		if len(prefix) > 17 {
			prefix = prefix[:17]
		}
		_, err := s.db.Exec(r.Context(), `INSERT INTO application_keys(id,application_id,label,environment,key_prefix,key_hash,permissions,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, appID, input.Label, input.Environment, prefix, hex.EncodeToString(hash[:]), permissions, expiresAt)
		if err != nil {
			fail(w, http.StatusConflict, "application key could not be created")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"status": true, "message": "Copy this key now; it will not be shown again.", "data": map[string]any{
			"key_id": id, "application_id": appID, "label": input.Label, "environment": input.Environment,
			"application_key": secret, "key_prefix": prefix, "permissions": input.Permissions, "expires_at": expiresAt,
		}})
		return
	}
	if len(parts) == 3 && parts[1] == "keys" && r.Method == http.MethodDelete {
		keyID := parts[2]
		tag, err := s.db.Exec(r.Context(), `UPDATE application_keys SET revoked_at=now() WHERE id=$1 AND application_id=$2 AND revoked_at IS NULL`, keyID, appID)
		if err != nil || tag.RowsAffected() != 1 {
			fail(w, http.StatusNotFound, "active application key not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": true, "data": map[string]string{"key_id": keyID, "status": "REVOKED"}})
		return
	}
	if len(parts) == 3 && parts[1] == "keys" && r.Method == http.MethodPatch {
		var input struct {
			Permissions []string `json:"permissions"`
		}
		if decodeJSON(r, &input) != nil || len(input.Permissions) == 0 || len(input.Permissions) > len(applicationKeyPermissions) {
			fail(w, http.StatusUnprocessableEntity, "select at least one supported permission")
			return
		}
		seen := map[string]bool{}
		for _, permission := range input.Permissions {
			if !applicationKeyPermissions[permission] || seen[permission] {
				fail(w, http.StatusUnprocessableEntity, "permissions contains an unsupported or duplicate value")
				return
			}
			seen[permission] = true
		}
		permissions, _ := json.Marshal(input.Permissions)
		tag, err := s.db.Exec(r.Context(), `UPDATE application_keys SET permissions=$1 WHERE id=$2 AND application_id=$3 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, permissions, parts[2], appID)
		if err != nil || tag.RowsAffected() != 1 {
			fail(w, http.StatusNotFound, "active application key not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": true, "data": map[string]any{"key_id": parts[2], "permissions": input.Permissions}})
		return
	}
	fail(w, http.StatusMethodNotAllowed, "method not allowed")
}

func hasApplicationPermission(a actor, permission string) bool {
	if a.Admin { return false }
	for _, granted := range a.Permissions {
		if granted == permission { return true }
	}
	return false
}

func requiredApplicationPermission(r *http.Request) string {
	path, method := r.URL.Path, r.Method
	switch {
	case path == "/api/v1/applications/me":
		return "application.read"
	case path == "/api/v1/assets" && method == http.MethodGet:
		return "assets.read"
	case path == "/api/v1/assets" && method == http.MethodPost:
		return "assets.upload"
	case strings.HasPrefix(path, "/api/v1/assets/"):
		parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/v1/assets/"), "/"), "/")
		if len(parts) == 1 && method == http.MethodGet { return "assets.read" }
		if len(parts) == 1 && method == http.MethodDelete { return "assets.delete" }
		if len(parts) == 2 && parts[1] == "content" && (method == http.MethodGet || method == http.MethodHead) { return "assets.read" }
		if len(parts) == 2 && parts[1] == "cancel-deletion" && method == http.MethodPost { return "assets.delete" }
		if len(parts) == 2 && parts[1] == "references" && method == http.MethodGet { return "references.read" }
		if len(parts) == 2 && parts[1] == "references" && (method == http.MethodPost || method == http.MethodDelete) { return "references.write" }
		if len(parts) == 2 && parts[1] == "grants" && (method == http.MethodGet || method == http.MethodPost || method == http.MethodDelete) { return "grants.manage" }
	}
	return ""
}
