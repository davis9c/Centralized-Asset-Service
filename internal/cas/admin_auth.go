package cas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	// Exactly one concurrently successful first login can claim the initial CAS admin role.
	_, err = s.db.Exec(r.Context(), `INSERT INTO cas_admins(user_id,role) SELECT $1,'SUPER_ADMIN' WHERE NOT EXISTS (SELECT 1 FROM cas_admins WHERE role='SUPER_ADMIN') ON CONFLICT DO NOTHING`, user.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "administrator setup failed")
		return
	}
	var localAdmin bool
	if err = s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM cas_admins WHERE user_id=$1 AND role='SUPER_ADMIN')`, user.ID).Scan(&localAdmin); err != nil {
		fail(w, http.StatusInternalServerError, "administrator setup failed")
		return
	}
	if !localAdmin && !user.SuperAdmin {
		fail(w, http.StatusForbidden, "login succeeded, but this user is not a CAS administrator")
		return
	}
	roles := append([]string(nil), user.Roles...)
	if localAdmin && !contains(roles, "SUPER_ADMIN") {
		roles = append(roles, "SUPER_ADMIN")
	}
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

