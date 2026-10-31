package cas

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Service) listAssets(w http.ResponseWriter, r *http.Request, a actor) {
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			fail(w, http.StatusUnprocessableEntity, "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 100000 {
			fail(w, http.StatusUnprocessableEntity, "offset must be between 0 and 100000")
			return
		}
		offset = parsed
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 200 {
		fail(w, http.StatusUnprocessableEntity, "q must be at most 200 characters")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	filter := "%" + query + "%"
	var total int64
	err := s.db.QueryRow(r.Context(), `SELECT count(*) FROM assets WHERE owner_application_id=$1 AND ($2='' OR status=$2) AND ($3='' OR original_filename ILIKE $4 OR id ILIKE $4)`, a.AppID, status, query, filter).Scan(&total)
	if err != nil {
		fail(w, http.StatusInternalServerError, "assets unavailable")
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT a.id,a.owner_application_id,a.original_filename,a.mime_type,a.size_bytes,a.checksum,a.storage_key,a.status,a.metadata,a.created_at,COALESCE(a.storage_node_id,''),a.storage_state,a.replica_target,(SELECT count(*) FROM asset_replicas r WHERE r.asset_id=a.id AND r.state='READY') FROM assets a WHERE a.owner_application_id=$1 AND ($2='' OR a.status=$2) AND ($3='' OR a.original_filename ILIKE $4 OR a.id ILIKE $4) ORDER BY a.created_at DESC,a.id DESC LIMIT $5 OFFSET $6`, a.AppID, status, query, filter, limit, offset)
	if err != nil {
		fail(w, http.StatusInternalServerError, "assets unavailable")
		return
	}
	defer rows.Close()
	assets := []map[string]any{}
	for rows.Next() {
		var item asset
		var created time.Time
		if err := rows.Scan(&item.ID, &item.Owner, &item.Name, &item.MIME, &item.Size, &item.Checksum, &item.Storage, &item.Status, &item.Metadata, &created,&item.NodeID,&item.StorageState,&item.ReplicaTarget,&item.ReadyReplicas); err != nil {
			fail(w, http.StatusInternalServerError, "assets unavailable")
			return
		}
		data := assetJSON(item)
		data["created_at"] = created
		assets = append(assets, data)
	}
	if rows.Err() != nil {
		fail(w, http.StatusInternalServerError, "assets unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": assets, "pagination": map[string]any{"total": total, "limit": limit, "offset": offset}})
}
