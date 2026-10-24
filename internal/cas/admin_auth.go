package cas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type userGateUser struct {
	ID        string   `json:"id"`
	Username  string   `json:"username"`
	FullName  string   `json:"full_name"`
	Status    string   `json:"status"`
	Roles     []string `json:"roles"`
	SuperAdmin bool    `json:"is_super_admin"`
}

func (s *Service) adminLogin(w http.ResponseWriter, r *http.Request) {
	if s.userGate.APIKey == "" {
		fail(w, http.StatusServiceUnavailable, "admin login is not configured")
		return
	}
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decodeJSON(r, &credentials) != nil || strings.TrimSpace(credentials.Username) == "" || credentials.Password == "" {
		fail(w, http.StatusUnprocessableEntity, "username and password are required")
		return
	}
	payload, _ := json.Marshal(credentials)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.userGate.BaseURL+"/auth/login", bytes.NewReader(payload))
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "login service is unavailable")
		return
	}
	req.Header.Set("X-API-Key", s.userGate.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := userGateHTTP.Do(req)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "login service is unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			fail(w, http.StatusUnauthorized, "invalid credentials")
		} else {
			fail(w, http.StatusServiceUnavailable, "login service is unavailable")
		}
		return
	}
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&login) != nil || login.Data.AccessToken == "" {
		fail(w, http.StatusBadGateway, "login service returned an invalid response")
		return
	}

	user, err := s.userGateMe(r.Context(), login.Data.AccessToken)
	if err != nil || user.ID == "" {
		fail(w, http.StatusBadGateway, "login service could not verify the user")
		return
	}
	if user.Username == "" { user.Username = credentials.Username }
	if user.FullName == "" { user.FullName = user.Username }
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "local user lookup failed")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(7410291)`); err != nil {
		fail(w, http.StatusInternalServerError, "local user lookup failed")
		return
	}
	var total int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM cas_local_users`).Scan(&total); err != nil {
		fail(w, http.StatusInternalServerError, "local user lookup failed")
		return
	}
	if total == 0 {
		_, err = tx.Exec(r.Context(), `INSERT INTO cas_local_users(user_id,username,full_name,role,created_by) VALUES($1,$2,$3,'SUPER_ADMIN','first-login') ON CONFLICT(user_id) DO NOTHING`, user.ID, user.Username, user.FullName)
		if err != nil {
			fail(w, http.StatusInternalServerError, "initial administrator setup failed")
			return
		}
	}
	var role string
	if err = tx.QueryRow(r.Context(), `SELECT COALESCE(role,'') FROM cas_local_users WHERE user_id=$1`, user.ID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, http.StatusForbidden, "User tidak terdaftar atau belum aktif. Hubungi administrator.")
		} else {
			fail(w, http.StatusInternalServerError, "local user lookup failed")
		}
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE cas_local_users SET username=$2,full_name=$3,updated_at=now() WHERE user_id=$1 AND role IS NOT NULL`, user.ID, user.Username, user.FullName); err != nil {
		fail(w, http.StatusInternalServerError, "local user update failed")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, http.StatusInternalServerError, "local user update failed")
		return
	}
	if !validLocalRole(role) {
		fail(w, http.StatusForbidden, "User tidak terdaftar atau belum aktif. Hubungi administrator.")
		return
	}
	roles := []string{role}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": true,
		"message": "authenticated successfully",
		"data": map[string]any{
			"access_token": login.Data.AccessToken,
			"user": map[string]any{"id": user.ID, "username": user.Username, "full_name": user.FullName, "roles": roles},
		},
	})
}

func (s *Service) userGateMe(ctx context.Context, token string) (userGateUser, error) {
	var user userGateUser
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.userGate.BaseURL+"/auth/me", nil)
	if err != nil {
		return user, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-API-Key", s.userGate.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := userGateHTTP.Do(req)
	if err != nil {
		return user, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return user, fmt.Errorf("usergate rejected access token")
	}
	var body struct {
		Data userGateUser `json:"data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&body); err != nil {
		return user, err
	}
	user = body.Data
	user.SuperAdmin = user.SuperAdmin || contains(user.Roles, "SUPER_ADMIN")
	return user, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validLocalRole(role string) bool {
	switch role {
	case "SUPER_ADMIN", "ADMIN", "AUDITOR":
		return true
	default:
		return false
	}
}

