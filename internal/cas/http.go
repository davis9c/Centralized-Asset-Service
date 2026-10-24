package cas

import (
	"encoding/json"
	"net/http"
	"strings"
)

func cors(allowedOrigin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Add("Vary", "Origin")
			if r.Method == http.MethodOptions && strings.HasPrefix(r.URL.Path, "/api/v1/") {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, PATCH, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Application-Key")
				w.Header().Set("Access-Control-Max-Age", "600")
				if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
					w.Header().Set("Access-Control-Allow-Private-Network", "true")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) applicationMe(w http.ResponseWriter, r *http.Request, a actor) {
	var id, name, status string
	err := s.db.QueryRow(r.Context(), `SELECT id,name,status FROM applications WHERE id=$1`, a.AppID).Scan(&id, &name, &status)
	if err != nil { fail(w, http.StatusUnauthorized, "application unavailable"); return }
	writeJSON(w, http.StatusOK, map[string]any{"status": true, "data": map[string]string{"application_id": id, "name": name, "status": status}})
}

func writeJSON(w http.ResponseWriter,status int,v any){w.Header().Set("Content-Type","application/json; charset=utf-8");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(v)}
func fail(w http.ResponseWriter,status int,message string){writeJSON(w,status,map[string]any{"status":false,"message":message})}
func requestID(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){id:=strings.TrimSpace(r.Header.Get("X-Request-ID"));if len(id)>100{id=""};if id==""{id=newID()};r.Header.Set("X-Request-ID",id);w.Header().Set("X-Request-ID",id);next.ServeHTTP(w,r)})}
func securityHeaders(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Referrer-Policy","no-referrer");w.Header().Set("Cache-Control","no-store");next.ServeHTTP(w,r)})}
