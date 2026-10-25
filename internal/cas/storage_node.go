package cas

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// StorageNodeHandler exposes the private, token-authenticated API for one disk node.
func StorageNodeHandler(root, token string) http.Handler {
	_ = os.MkdirAll(root, 0700)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"status": true, "data": map[string]string{"state": "ok"}}) })
	mux.HandleFunc("GET /internal/v1/capacity", func(w http.ResponseWriter, r *http.Request) {
		var st syscall.Statfs_t
		if err := syscall.Statfs(root, &st); err != nil { fail(w, 500, "capacity unavailable"); return }
		total := st.Blocks * uint64(st.Bsize)
		available := st.Bavail * uint64(st.Bsize)
		used := total - available
		writeJSON(w, 200, map[string]uint64{"capacity_bytes": total, "used_bytes": used, "available_bytes": available, "reserved_bytes": 0})
	})
	mux.HandleFunc("/internal/v1/objects/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if key == "" || strings.ContainsAny(key, `/\\.`) { fail(w, 400, "invalid object key"); return }
		path := filepath.Join(root, key)
		switch r.Method {
		case http.MethodPut:
			wantHash, wantSize := strings.ToLower(r.Header.Get("X-Checksum-SHA256")), r.Header.Get("X-Object-Size")
			if len(wantHash) != 64 || wantSize == "" { fail(w, 400, "checksum and size are required"); return }
			if r.ContentLength < 0 || fmt.Sprint(r.ContentLength) != wantSize { fail(w, 400, "content length does not match object size"); return }
			if r.ContentLength > parseEnvInt64("CAS_NODE_MAX_OBJECT_BYTES", 10<<30) { fail(w, 413, "object exceeds storage node limit"); return }
			replaceCorrupt := false
			if f, err := os.Open(path); err == nil {
				h := sha256.New(); n, e := io.Copy(h, f); _ = f.Close()
				if e == nil && hex.EncodeToString(h.Sum(nil)) == wantHash && fmt.Sprint(n) == wantSize { w.WriteHeader(http.StatusNoContent); return }
				replaceCorrupt = true
			} else if !os.IsNotExist(err) { fail(w, 500, "existing object could not be checked"); return }
			tmp, err := os.CreateTemp(root, ".upload-*"); if err != nil { fail(w, 500, "storage unavailable"); return }
			tmpPath := tmp.Name(); defer os.Remove(tmpPath)
			h := sha256.New(); n, err := io.Copy(io.MultiWriter(tmp, h), http.MaxBytesReader(w, r.Body, r.ContentLength))
			if err == nil { err = tmp.Sync() }; closeErr := tmp.Close()
			if err != nil || closeErr != nil || fmt.Sprint(n) != wantSize || hex.EncodeToString(h.Sum(nil)) != wantHash { fail(w, 422, "size or checksum mismatch"); return }
			if replaceCorrupt {
				if err = os.Rename(tmpPath, path); err != nil { fail(w, 500, "corrupt object could not be replaced"); return }
			} else {
				if err = os.Link(tmpPath, path); err != nil { if os.IsExist(err) { fail(w, 409, "object key already exists"); return }; fail(w, 500, "object could not be committed"); return }
				if err = os.Remove(tmpPath); err != nil { fail(w, 500, "object could not be committed"); return }
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet, http.MethodHead:
			f, err := os.Open(path); if err != nil { http.NotFound(w, r); return }; defer f.Close()
			st, err := f.Stat(); if err != nil { fail(w, 500, "object unavailable"); return }
			h := sha256.New(); if _, err = io.Copy(h, f); err != nil { fail(w, 500, "object unavailable"); return }
			w.Header().Set("X-Checksum-SHA256", hex.EncodeToString(h.Sum(nil))); w.Header().Set("Content-Length", fmt.Sprint(st.Size()))
			if r.Method == http.MethodHead { w.WriteHeader(200); return }
			_, _ = f.Seek(0, 0); http.ServeContent(w, r, key, st.ModTime(), f)
		case http.MethodDelete:
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) { fail(w, 500, "object could not be removed"); return }; w.WriteHeader(http.StatusNoContent)
		default: fail(w, 405, "method not allowed")
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided:=strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(provided),[]byte(token)) != 1 { fail(w, 401, "unauthorized"); return }
		mux.ServeHTTP(w, r)
	})
}

func (s *Service) ConfigureStorageNode(baseURL, token, caFile, nodeID string) error {
	return s.ConfigureStorageNodes([]StorageNodeConfig{{ID:nodeID,URL:baseURL,Token:token,CAFile:caFile,FailureDomain:nodeID}})
}
