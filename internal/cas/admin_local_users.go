package cas

import (
	"net/http"
	"strings"
)

func (s *Service) adminLocalUsers(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/admin/local-users" {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/local-users/")
		if id == "" || strings.Contains(id, "/") {
			fail(w, http.StatusNotFound, "local user not found")
			return
		}
		if r.Method != http.MethodPatch {
			fail(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var in struct { Role string `json:"role"` }
		if decodeJSON(r, &in) != nil || (in.Role != "" && !validLocalRole(in.Role)) {
			fail(w, http.StatusUnprocessableEntity, "role must be SUPER_ADMIN, ADMIN, AUDITOR, or empty")
			return
		}
		s.updateLocalUserRole(w, r, id, in.Role)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listLocalUsers(w, r)
	default:
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) listLocalUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT user_id,username,full_name,COALESCE(role,''),created_at FROM cas_local_users ORDER BY created_at,user_id`)
	if err != nil { fail(w, http.StatusInternalServerError, "local users unavailable"); return }
	defer rows.Close()
	users := []map[string]any{}
	for rows.Next() {
		var id, username, fullName, role string
		var created any
		if rows.Scan(&id, &username, &fullName, &role, &created) == nil {
			users = append(users, map[string]any{"user_id": id, "username": username, "full_name": fullName, "role": role, "active": validLocalRole(role), "created_at": created})
		}
	}
	if rows.Err() != nil { fail(w, http.StatusInternalServerError, "local users unavailable"); return }
	writeJSON(w, http.StatusOK, map[string]any{"data": users})
}

func (s *Service) updateLocalUserRole(w http.ResponseWriter, r *http.Request, id, role string) {
	tx, err := s.db.Begin(r.Context())
	if err != nil { fail(w, http.StatusInternalServerError, "local user update failed"); return }
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(7410291)`); err != nil { fail(w, http.StatusInternalServerError, "local user update failed"); return }
	var oldRole string
	err = tx.QueryRow(r.Context(), `SELECT COALESCE(role,'') FROM cas_local_users WHERE user_id=$1 FOR UPDATE`, id).Scan(&oldRole)
	if err != nil { fail(w, http.StatusNotFound, "local user not found"); return }
	if oldRole == "SUPER_ADMIN" && role != "SUPER_ADMIN" {
		var count int
		if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM cas_local_users WHERE role='SUPER_ADMIN'`).Scan(&count); err != nil { fail(w, http.StatusInternalServerError, "local user update failed"); return }
		if count <= 1 { fail(w, http.StatusConflict, "at least one SUPER_ADMIN must remain active"); return }
	}
	var roleValue any
	if role != "" { roleValue = role }
	if _, err = tx.Exec(r.Context(), `UPDATE cas_local_users SET role=$2,updated_at=now() WHERE user_id=$1`, id, roleValue); err != nil { fail(w, http.StatusInternalServerError, "local user update failed"); return }
	if err = tx.Commit(r.Context()); err != nil { fail(w, http.StatusInternalServerError, "local user update failed"); return }
	a := r.Context().Value(actorKey).(actor)
	s.audit(r, a, "local_user.role.update", "", "success")
	writeJSON(w, http.StatusOK, map[string]any{"status": true, "data": map[string]any{"user_id": id, "role": role, "active": validLocalRole(role)}})
}
