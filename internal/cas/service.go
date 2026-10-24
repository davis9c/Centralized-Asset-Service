package cas

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserGate struct { BaseURL, APIKey string }
type Service struct { db *pgxpool.Pool; storage string; maxUpload int64; userGate UserGate }
type actor struct { AppID string; Admin bool; UserID string }
type ctxKey int
const actorKey ctxKey = 1
var userGateHTTP = &http.Client{Timeout: 5 * time.Second}

func New(db *pgxpool.Pool, storage string, max int64, ug UserGate) *Service { return &Service{db:db, storage:storage, maxUpload:max, userGate:ug} }
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect) })
	mux.HandleFunc("GET /admin/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/" { http.NotFound(w, r); return }
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		_, _ = w.Write(adminHTML)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" { http.NotFound(w, r); return }
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, `<!doctype html><html lang="id"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>CAS</title><style>body{font:16px system-ui,sans-serif;max-width:760px;margin:10vh auto;padding:0 24px;color:#17212b}h1{font-size:2.2rem;margin-bottom:.25rem}p{line-height:1.6;color:#425466}code{background:#f1f4f7;padding:3px 6px;border-radius:4px}a{color:#0759b8}li{margin:.6rem 0}</style></head><body><h1>Centralized Asset Service</h1><p>CAS API aktif.</p><p><a href="/admin/">Buka dashboard admin</a> · <a href="/healthz">Health check</a></p><h2>Endpoint utama</h2><ul><li><code>POST /api/v1/assets</code> — upload file</li><li><code>GET /api/v1/assets/{id}</code> — metadata aset</li><li><code>GET /api/v1/assets/{id}/content</code> — download atau streaming</li></ul><p>API memerlukan autentikasi. Lihat README proyek untuk kontrak dan konfigurasi.</p></body></html>`)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w,200,map[string]any{"status":true,"data":map[string]string{"state":"ok"}}) })
	mux.Handle("/api/v1/", http.HandlerFunc(s.route))
	return requestID(securityHeaders(mux))
}
func (s *Service) route(w http.ResponseWriter, r *http.Request) {
	var a actor
	var err error
	if strings.HasPrefix(r.URL.Path,"/api/v1/admin/") { a,err=s.authenticateAdmin(r) } else { a,err=s.authenticateApp(r) }
	if err != nil { if err.Error()=="forbidden" { fail(w,403,"forbidden") } else { fail(w,401,"unauthorized") }; return }
	r = contextWithActor(r,a)
	switch {
	case r.Method==http.MethodPost && r.URL.Path=="/api/v1/assets": s.upload(w,r,a)
	case strings.HasPrefix(r.URL.Path,"/api/v1/assets/"): s.assetRoutes(w,r,a)
	case r.URL.Path=="/api/v1/admin/applications" && a.Admin: s.adminApplications(w,r)
	case strings.HasPrefix(r.URL.Path,"/api/v1/admin/applications/") && a.Admin: s.adminApplication(w,r)
	case r.URL.Path=="/api/v1/admin/assets" && r.Method==http.MethodGet && a.Admin: s.adminAssets(w,r)
	case r.URL.Path=="/api/v1/admin/audit" && r.Method==http.MethodGet && a.Admin: s.adminAudit(w,r)
	default: fail(w,404,"not found")
	}
}
func (s *Service) authenticateApp(r *http.Request) (actor,error) {
	key:=r.Header.Get("X-Application-Key"); if len(key)<32 { return actor{},fmt.Errorf("unauthorized") }
	h:=sha256.Sum256([]byte(key)); var id,status string
	err:=s.db.QueryRow(r.Context(),`SELECT id,status FROM applications WHERE key_hash=$1`,hex.EncodeToString(h[:])).Scan(&id,&status)
	if err!=nil || status!="ACTIVE" { return actor{},fmt.Errorf("unauthorized") }
	return actor{AppID:id},nil
}
func (s *Service) authenticateAdmin(r *http.Request) (actor,error) {
	if s.userGate.APIKey=="" { return actor{},fmt.Errorf("admin auth not configured") }
	token:=strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer ")); if token=="" { return actor{},fmt.Errorf("missing token") }
	req,err:=http.NewRequestWithContext(r.Context(),"GET",s.userGate.BaseURL+"/auth/me",nil); if err!=nil{return actor{},err}
	req.Header.Set("Authorization","Bearer "+token); req.Header.Set("X-API-Key",s.userGate.APIKey); req.Header.Set("Accept","application/json")
	resp,err:=userGateHTTP.Do(req); if err!=nil{return actor{},err}; defer resp.Body.Close(); if resp.StatusCode!=200{return actor{},fmt.Errorf("invalid admin token")}
	var body struct{ Data struct{ ID string `json:"id"`; Super bool `json:"is_super_admin"`; Roles []string `json:"roles"` } `json:"data"` }
	if json.NewDecoder(http.MaxBytesReader(nil,resp.Body,1<<20)).Decode(&body)!=nil{return actor{},fmt.Errorf("invalid usergate response")}
	admin:=body.Data.Super; for _,role:=range body.Data.Roles { if role=="SUPER_ADMIN" {admin=true} }; if !admin{return actor{},fmt.Errorf("forbidden")}
	return actor{Admin:true,UserID:body.Data.ID},nil
}
func (s *Service) upload(w http.ResponseWriter,r *http.Request,a actor) {
	r.Body=http.MaxBytesReader(w,r.Body,s.maxUpload+(1<<20))
	if err:=r.ParseMultipartForm(s.maxUpload);err!=nil { fail(w,413,"file exceeds configured upload limit"); return }
	file,h,err:=r.FormFile("file"); if err!=nil { fail(w,422,"file field is required"); return }; defer file.Close()
	if h.Size<=0 || h.Size>s.maxUpload {fail(w,413,"invalid file size");return}
	name:=filepath.Base(strings.ReplaceAll(h.Filename,"\\","/")); if name=="." || name=="/" || len(name)>255 {fail(w,422,"invalid filename");return}
	id:=newID(); key:=id
	dst,err:=os.OpenFile(filepath.Join(s.storage,key),os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600); if err!=nil {fail(w,500,"storage unavailable");return}
	hash:=sha256.New(); n,copyErr:=io.Copy(io.MultiWriter(dst,hash),io.LimitReader(file,s.maxUpload+1)); closeErr:=dst.Close()
	if copyErr!=nil||closeErr!=nil||n<=0||n>s.maxUpload { _=os.Remove(filepath.Join(s.storage,key));fail(w,500,"upload failed");return }
	mime:="application/octet-stream"
	if sniff,openErr:=os.Open(filepath.Join(s.storage,key));openErr==nil { head:=make([]byte,512); count,_:=sniff.Read(head); if count>0{mime=http.DetectContentType(head[:count])}; _=sniff.Close() }
	meta:=r.FormValue("metadata"); if meta=="" {meta="{}"}; var metadata map[string]any
	if len(meta)>64*1024 || json.Unmarshal([]byte(meta),&metadata)!=nil || metadata==nil { _=os.Remove(filepath.Join(s.storage,key)); fail(w,422,"metadata must be a JSON object up to 64 KiB");return }
	_,err=s.db.Exec(r.Context(),`INSERT INTO assets(id,owner_application_id,original_filename,mime_type,size_bytes,checksum,storage_key,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,id,a.AppID,name,mime,n,hex.EncodeToString(hash.Sum(nil)),key,metadata)
	if err!=nil { _=os.Remove(filepath.Join(s.storage,key));fail(w,500,"asset could not be registered");return }
	s.audit(r,a,"asset.upload",id,"success")
	writeJSON(w,201,map[string]any{"status":true,"message":"asset created","data":map[string]any{"asset_id":id,"owner_application_id":a.AppID,"original_filename":name,"mime_type":mime,"size_bytes":n,"checksum":hex.EncodeToString(hash.Sum(nil)),"asset_status":"ACTIVE"}})
}
func (s *Service) assetRoutes(w http.ResponseWriter,r *http.Request,a actor) {
	parts:=strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path,"/api/v1/assets/"),"/"),"/"); if len(parts)==0||parts[0]=="" {fail(w,404,"not found");return}; id:=parts[0]
	if len(parts)==2 && parts[1]=="content" && (r.Method=="GET"||r.Method=="HEAD") {s.content(w,r,a,id);return}
	if len(parts)==2 && parts[1]=="references" {s.references(w,r,a,id);return}
	if len(parts)==2 && parts[1]=="grants" {s.grants(w,r,a,id);return}
	if len(parts)==2 && parts[1]=="cancel-deletion" && r.Method=="POST" {s.cancelDeletion(w,r,a,id);return}
	if len(parts)!=1 {fail(w,404,"not found");return}
	switch r.Method {case "GET":s.getAsset(w,r,a,id);case "DELETE":s.requestDeletion(w,r,a,id);default:fail(w,405,"method not allowed")}
}
type asset struct { ID,Owner,Name,MIME,Checksum,Storage,Status string; Size int64; Metadata json.RawMessage }
func (s *Service) loadAsset(r *http.Request,id string)(asset,error){ var x asset; err:=s.db.QueryRow(r.Context(),`SELECT id,owner_application_id,original_filename,mime_type,size_bytes,checksum,storage_key,status,metadata FROM assets WHERE id=$1`,id).Scan(&x.ID,&x.Owner,&x.Name,&x.MIME,&x.Size,&x.Checksum,&x.Storage,&x.Status,&x.Metadata);return x,err }
func (s *Service) allowed(r *http.Request,a actor,x asset,action string) bool {
	if a.Admin{return false} // administrator content access remains a separate policy decision
	if x.Status!="ACTIVE" && x.Status!="DELETE_PENDING" && x.Status!="DELETE_BLOCKED" && x.Status!="PURGE_PENDING" { return false }
	if a.AppID==x.Owner{return true}
	var ok bool
	err:=s.db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM access_grants g WHERE g.asset_id=$1 AND g.application_id=$2 AND g.action=$3 AND g.active)`,x.ID,a.AppID,action).Scan(&ok)
	if err!=nil||!ok{return false}; if x.Status=="ACTIVE" {return true}
	if x.Status=="DELETE_PENDING"||x.Status=="DELETE_BLOCKED"||x.Status=="PURGE_PENDING" {var ref bool; _=s.db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM asset_references WHERE asset_id=$1 AND application_id=$2 AND active)`,x.ID,a.AppID).Scan(&ref);return ref}
	return false
}
func (s *Service) getAsset(w http.ResponseWriter,r *http.Request,a actor,id string){ x,e:=s.loadAsset(r,id); if e!=nil {fail(w,404,"asset not found");return}; if !s.allowed(r,a,x,"metadata.read") {fail(w,403,"access denied");return}; writeJSON(w,200,map[string]any{"status":true,"data":assetJSON(x)}) }
func assetJSON(x asset) map[string]any{return map[string]any{"asset_id":x.ID,"owner_application_id":x.Owner,"original_filename":x.Name,"mime_type":x.MIME,"size_bytes":x.Size,"checksum":x.Checksum,"asset_status":x.Status,"metadata":x.Metadata}}
func (s *Service) content(w http.ResponseWriter,r *http.Request,a actor,id string){ x,e:=s.loadAsset(r,id);if e!=nil{fail(w,404,"asset not found");return}; if !s.allowed(r,a,x,"content.read"){fail(w,403,"access denied");return}; f,e:=os.Open(filepath.Join(s.storage,x.Storage));if e!=nil{fail(w,500,"content unavailable");return};defer f.Close();w.Header().Set("Content-Type",x.MIME);disposition:="attachment";if r.URL.Query().Get("inline")=="true"&&previewable(x.MIME){disposition="inline"};w.Header().Set("Content-Disposition",fmt.Sprintf("%s; filename=%q",disposition,x.Name));w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("ETag",`"`+x.Checksum+`"`);http.ServeContent(w,r,x.Name,time.Time{},f) }
func previewable(mime string)bool{return strings.HasPrefix(mime,"image/")||strings.HasPrefix(mime,"audio/")||strings.HasPrefix(mime,"video/")||mime=="application/pdf"}
func (s *Service) requestDeletion(w http.ResponseWriter,r *http.Request,a actor,id string){x,e:=s.loadAsset(r,id);if e!=nil{fail(w,404,"asset not found");return};if x.Owner!=a.AppID{fail(w,403,"only owner may request deletion");return};if x.Status!="ACTIVE"{fail(w,409,"asset is not active");return};var refs int;_ = s.db.QueryRow(r.Context(),`SELECT count(*) FROM asset_references WHERE asset_id=$1 AND active`,id).Scan(&refs);status:="DELETE_PENDING";if refs>0{status="DELETE_BLOCKED"};tag,e:=s.db.Exec(r.Context(),`UPDATE assets SET status=$2,delete_requested_by=$3,delete_requested_at=now() WHERE id=$1 AND status='ACTIVE'`,id,status,a.AppID);if e!=nil||tag.RowsAffected()!=1{fail(w,409,"asset state changed");return};s.audit(r,a,"asset.delete.request",id,"success");writeJSON(w,202,map[string]any{"status":true,"data":map[string]any{"asset_id":id,"asset_status":status,"active_references":refs}})}
func (s *Service) cancelDeletion(w http.ResponseWriter,r *http.Request,a actor,id string){x,e:=s.loadAsset(r,id);if e!=nil{fail(w,404,"asset not found");return};if x.Owner!=a.AppID{fail(w,403,"only owner may cancel deletion");return};tag,e:=s.db.Exec(r.Context(),`UPDATE assets SET status='ACTIVE',delete_requested_by=NULL,delete_requested_at=NULL WHERE id=$1 AND status IN ('DELETE_PENDING','DELETE_BLOCKED','PURGE_PENDING')`,id);if e!=nil||tag.RowsAffected()!=1{fail(w,409,"deletion can no longer be cancelled");return};s.audit(r,a,"asset.delete.cancel",id,"success");writeJSON(w,200,map[string]any{"status":true,"data":map[string]string{"asset_id":id,"asset_status":"ACTIVE"}})}
func (s *Service) references(w http.ResponseWriter,r *http.Request,a actor,id string){x,e:=s.loadAsset(r,id);if e!=nil{fail(w,404,"asset not found");return};if r.Method=="POST"{if x.Status!="ACTIVE"{fail(w,409,"new references are blocked while deletion is pending");return};if x.Owner!=a.AppID{var ok bool;_ = s.db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM access_grants WHERE asset_id=$1 AND application_id=$2 AND action='reference.create' AND active)`,id,a.AppID).Scan(&ok);if !ok{fail(w,403,"reference.create grant required");return}};var in struct{EntityID string `json:"entity_id"`};if decodeJSON(r,&in)!=nil||strings.TrimSpace(in.EntityID)==""||len(in.EntityID)>255{fail(w,422,"entity_id is required");return};rid:=newID();_,e=s.db.Exec(r.Context(),`INSERT INTO asset_references(id,asset_id,application_id,entity_id) VALUES($1,$2,$3,$4)`,rid,id,a.AppID,in.EntityID);if e!=nil{fail(w,500,"reference could not be created");return};s.audit(r,a,"asset.reference.create",id,"success");writeJSON(w,201,map[string]string{"reference_id":rid,"asset_id":id,"entity_id":in.EntityID,"status":"ACTIVE"});return};if r.Method=="GET"{if x.Owner!=a.AppID {var ok bool;_ = s.db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM access_grants WHERE asset_id=$1 AND application_id=$2 AND action='reference.read' AND active)`,id,a.AppID).Scan(&ok);if !ok{fail(w,403,"access denied");return}};rows,e:=s.db.Query(r.Context(),`SELECT id,application_id,entity_id,active,created_at FROM asset_references WHERE asset_id=$1 ORDER BY created_at`,id);if e!=nil{fail(w,500,"references unavailable");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var rid,app,entity string;var active bool;var created time.Time;if rows.Scan(&rid,&app,&entity,&active,&created)==nil{out=append(out,map[string]any{"reference_id":rid,"application_id":app,"entity_id":entity,"active":active,"created_at":created})}};writeJSON(w,200,map[string]any{"data":out});return};if r.Method=="DELETE"&&len(strings.Split(strings.Trim(r.URL.Path,"/"),"/"))==5{rid:=strings.TrimSpace(r.URL.Query().Get("reference_id"));if rid==""{fail(w,422,"reference_id query is required");return};tag,e:=s.db.Exec(r.Context(),`UPDATE asset_references SET active=false,deactivated_at=now() WHERE id=$1 AND asset_id=$2 AND application_id=$3 AND active`,rid,id,a.AppID);if e!=nil||tag.RowsAffected()!=1{fail(w,404,"reference not found");return};s.audit(r,a,"asset.reference.deactivate",id,"success");s.refreshDeleteState(r,id);writeJSON(w,200,map[string]bool{"status":true});return};fail(w,405,"method not allowed")}
func (s *Service) grants(w http.ResponseWriter,r *http.Request,a actor,id string){x,e:=s.loadAsset(r,id);if e!=nil{fail(w,404,"asset not found");return};if x.Owner!=a.AppID{fail(w,403,"only owner may manage grants");return};if r.Method=="POST"{if x.Status!="ACTIVE"{fail(w,409,"new grants are blocked while deletion is pending");return};var in struct{ApplicationID string `json:"application_id"`;Action string `json:"action"`};if decodeJSON(r,&in)!=nil||in.ApplicationID==""||!validAction(in.Action){fail(w,422,"application_id and a supported action are required");return};if in.ApplicationID==a.AppID{fail(w,422,"owner does not need a grant");return};gid:=newID();_,e=s.db.Exec(r.Context(),`INSERT INTO access_grants(id,asset_id,application_id,action,granted_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT(asset_id,application_id,action) DO UPDATE SET active=true,revoked_at=NULL,granted_by=$5`,gid,id,in.ApplicationID,in.Action,a.AppID);if e!=nil{fail(w,422,"target application not found or grant invalid");return};s.audit(r,a,"asset.grant.create",id,"success");writeJSON(w,201,map[string]any{"status":true,"data":map[string]string{"application_id":in.ApplicationID,"action":in.Action,"active":"true"}});return};if r.Method=="GET"{rows,e:=s.db.Query(r.Context(),`SELECT id,application_id,action,active,created_at FROM access_grants WHERE asset_id=$1 ORDER BY created_at`,id);if e!=nil{fail(w,500,"grants unavailable");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var gid,app,action string;var active bool;var created time.Time;if rows.Scan(&gid,&app,&action,&active,&created)==nil{out=append(out,map[string]any{"grant_id":gid,"application_id":app,"action":action,"active":active,"created_at":created})}};writeJSON(w,200,map[string]any{"data":out});return};if r.Method=="DELETE"{gid:=r.URL.Query().Get("grant_id");tag,e:=s.db.Exec(r.Context(),`UPDATE access_grants SET active=false,revoked_at=now() WHERE id=$1 AND asset_id=$2 AND active`,gid,id);if e!=nil||tag.RowsAffected()!=1{fail(w,404,"grant not found");return};s.audit(r,a,"asset.grant.revoke",id,"success");writeJSON(w,200,map[string]bool{"status":true});return};fail(w,405,"method not allowed")}
func validAction(a string)bool{switch a{case "metadata.read","content.read","reference.read","reference.create":return true};return false}
func (s *Service) refreshDeleteState(r *http.Request,id string){var count int;_ = s.db.QueryRow(r.Context(),`SELECT count(*) FROM asset_references WHERE asset_id=$1 AND active`,id).Scan(&count);if count==0{_,_=s.db.Exec(r.Context(),`UPDATE assets SET status='DELETE_PENDING' WHERE id=$1 AND status='DELETE_BLOCKED'`,id)}}
func (s *Service) adminAssets(w http.ResponseWriter,r *http.Request){rows,e:=s.db.Query(r.Context(),`SELECT id,owner_application_id,original_filename,mime_type,size_bytes,checksum,storage_key,status,metadata FROM assets ORDER BY created_at DESC LIMIT 200`);if e!=nil{fail(w,500,"assets unavailable");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var x asset;if rows.Scan(&x.ID,&x.Owner,&x.Name,&x.MIME,&x.Size,&x.Checksum,&x.Storage,&x.Status,&x.Metadata)==nil{out=append(out,assetJSON(x))}};writeJSON(w,200,map[string]any{"data":out})}
func (s *Service) adminApplications(w http.ResponseWriter,r *http.Request){if r.Method=="GET"{rows,e:=s.db.Query(r.Context(),`SELECT id,name,status,created_at FROM applications ORDER BY created_at DESC`);if e!=nil{fail(w,500,"applications unavailable");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id,name,status string;var created time.Time;if rows.Scan(&id,&name,&status,&created)==nil{out=append(out,map[string]any{"application_id":id,"name":name,"status":status,"created_at":created})}};writeJSON(w,200,map[string]any{"data":out});return};if r.Method!="POST"{fail(w,405,"method not allowed");return};var in struct{ID string `json:"application_id"`;Name string `json:"name"`};if decodeJSON(r,&in)!=nil||strings.TrimSpace(in.ID)==""||strings.TrimSpace(in.Name)==""||len(in.ID)>100||len(in.Name)>150{fail(w,422,"application_id and name are required");return};key:=make([]byte,32);if _,e:=rand.Read(key);e!=nil{fail(w,500,"credential could not be generated");return};plain:=hex.EncodeToString(key);sum:=sha256.Sum256([]byte(plain));_,e:=s.db.Exec(r.Context(),`INSERT INTO applications(id,name,key_hash,status) VALUES($1,$2,$3,'ACTIVE')`,in.ID,in.Name,hex.EncodeToString(sum[:]));if e!=nil{fail(w,409,"application id already exists");return};a:=r.Context().Value(actorKey).(actor);s.audit(r,a,"application.create","","success");writeJSON(w,201,map[string]any{"status":true,"data":map[string]string{"application_id":in.ID,"name":in.Name,"status":"ACTIVE","application_key":plain},"message":"Simpan application_key sekarang; kunci hanya ditampilkan satu kali."})}
func (s *Service) adminApplication(w http.ResponseWriter,r *http.Request){id:=strings.TrimPrefix(r.URL.Path,"/api/v1/admin/applications/");if id==""||strings.Contains(id,"/"){fail(w,404,"application not found");return};if r.Method!="PATCH"{fail(w,405,"method not allowed");return};var in struct{Status string `json:"status"`};if decodeJSON(r,&in)!=nil||(in.Status!="ACTIVE"&&in.Status!="DISABLED"&&in.Status!="RETIRED"){fail(w,422,"status must be ACTIVE, DISABLED, or RETIRED");return};tag,e:=s.db.Exec(r.Context(),`UPDATE applications SET status=$2 WHERE id=$1`,id,in.Status);if e!=nil||tag.RowsAffected()!=1{fail(w,404,"application not found");return};a:=r.Context().Value(actorKey).(actor);s.audit(r,a,"application.status.update","","success");writeJSON(w,200,map[string]string{"application_id":id,"status":in.Status})}
func (s *Service) adminAudit(w http.ResponseWriter,r *http.Request){rows,e:=s.db.Query(r.Context(),`SELECT id,actor_application_id,action,asset_id,result,request_id,created_at FROM audit_events ORDER BY created_at DESC LIMIT 200`);if e!=nil{fail(w,500,"audit unavailable");return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var id,actor,action,result,rid string;var assetID *string;var created time.Time;if rows.Scan(&id,&actor,&action,&assetID,&result,&rid,&created)==nil{out=append(out,map[string]any{"id":id,"actor":actor,"action":action,"asset_id":assetID,"result":result,"request_id":rid,"created_at":created})}};writeJSON(w,200,map[string]any{"data":out})}
func (s *Service) audit(r *http.Request,a actor,action,assetID,result string){rid:=r.Header.Get("X-Request-ID");if rid==""{rid=newID()};_,_=s.db.Exec(r.Context(),`INSERT INTO audit_events(id,actor_application_id,action,asset_id,result,request_id) VALUES($1,$2,$3,$4,$5,$6)`,newID(),actorName(a),action,assetID,result,rid)}
func actorName(a actor)string{if a.Admin{return "user:"+a.UserID};return "app:"+a.AppID}
func contextWithActor(r *http.Request,a actor)*http.Request{return r.WithContext(context.WithValue(r.Context(),actorKey,a))}
func decodeJSON(r *http.Request,v any)error{r.Body=http.MaxBytesReader(nil,r.Body,1<<20);d:=json.NewDecoder(r.Body);d.DisallowUnknownFields();return d.Decode(v)}
func newID()string{b:=make([]byte,16);if _,e:=rand.Read(b);e!=nil{return strconv.FormatInt(time.Now().UnixNano(),16)};b[6]=(b[6]&0x0f)|0x40;b[8]=(b[8]&0x3f)|0x80;h:=hex.EncodeToString(b);return h[:8]+"-"+h[8:12]+"-"+h[12:16]+"-"+h[16:20]+"-"+h[20:]}
