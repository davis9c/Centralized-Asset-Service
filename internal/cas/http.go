package cas

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter,status int,v any){w.Header().Set("Content-Type","application/json; charset=utf-8");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(v)}
func fail(w http.ResponseWriter,status int,message string){writeJSON(w,status,map[string]any{"status":false,"message":message})}
func requestID(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){id:=strings.TrimSpace(r.Header.Get("X-Request-ID"));if len(id)>100{id=""};if id==""{id=newID()};r.Header.Set("X-Request-ID",id);w.Header().Set("X-Request-ID",id);next.ServeHTTP(w,r)})}
func securityHeaders(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Referrer-Policy","no-referrer");w.Header().Set("Cache-Control","no-store");next.ServeHTTP(w,r)})}
