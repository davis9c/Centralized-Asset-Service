package cas

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

)

type StorageNodeConfig struct {
	ID string `json:"id"`
	URL string `json:"url"`
	FailureDomain string `json:"failure_domain"`
	Token string `json:"token"`
	CAFile string `json:"ca_file"`
}

type storageNodeClient struct { StorageNodeConfig; HTTP *http.Client }

func ParseStorageNodeConfigs(raw string) ([]StorageNodeConfig, error) {
	var configs []StorageNodeConfig
	if err := json.Unmarshal([]byte(raw), &configs); err != nil { return nil, fmt.Errorf("invalid CAS_STORAGE_NODES JSON") }
	if len(configs) == 0 { return nil, fmt.Errorf("CAS_STORAGE_NODES must contain at least one node") }
	return configs, nil
}

func (s *Service) ConfigureStorageNodes(configs []StorageNodeConfig) error {
	s.nodes = make(map[string]storageNodeClient, len(configs))
	for _, c := range configs {
		u, err := url.Parse(c.URL)
		if strings.TrimSpace(c.ID)=="" || strings.TrimSpace(c.FailureDomain)=="" || err!=nil || u.Scheme!="https" || u.Host=="" || c.Token=="" { return fmt.Errorf("each storage node requires id, failure domain, HTTPS URL, and service token") }
		if _, ok := s.nodes[c.ID]; ok { return fmt.Errorf("duplicate storage node id") }
		tr := &http.Transport{TLSHandshakeTimeout:10*time.Second, ResponseHeaderTimeout:30*time.Second}
		tr.TLSClientConfig=&tls.Config{MinVersion:tls.VersionTLS12}
		if c.CAFile!="" { pem,err:=os.ReadFile(c.CAFile); if err!=nil{return err}; roots,err:=x509.SystemCertPool();if err!=nil||roots==nil{roots=x509.NewCertPool()};if !roots.AppendCertsFromPEM(pem){return fmt.Errorf("invalid CA certificate for storage node %s",c.ID)};tr.TLSClientConfig.RootCAs=roots }
		s.nodes[c.ID]=storageNodeClient{StorageNodeConfig:c, HTTP:&http.Client{Transport:tr}}
	}
	return nil
}

func (s *Service) RegisterStorageNodes(ctx context.Context) error {
	for _, n := range s.nodes {
		if _,err:=s.db.Exec(ctx,`INSERT INTO storage_nodes(id,name,base_url,failure_domain,status) VALUES($1,$1,$2,$3,'OFFLINE') ON CONFLICT(id) DO UPDATE SET base_url=EXCLUDED.base_url,failure_domain=EXCLUDED.failure_domain`,n.ID,n.URL,n.FailureDomain);err!=nil{return err}
	}
	return nil
}

func (s *Service) SyncStorageNodes(ctx context.Context) { s.heartbeatNodes(ctx) }

func (s *Service) StartStorageWorkers(ctx context.Context) {
	if len(s.nodes)==0 { return }
	go func(){ ticker:=time.NewTicker(20*time.Second);defer ticker.Stop();for{ s.heartbeatNodes(ctx);select{case <-ctx.Done():return;case <-ticker.C:} } }()
	go func(){ ticker:=time.NewTicker(2*time.Second);defer ticker.Stop();for{ if err:=s.replicateOne(ctx);err!=nil{/* durable retry state is updated in replicateOne */};if err:=s.deleteOne(ctx);err!=nil{/* durable retry state is updated in deleteOne */};select{case <-ctx.Done():return;case <-ticker.C:} } }()
	go func(){ ticker:=time.NewTicker(5*time.Minute);defer ticker.Stop();for{ s.reconcileReplicas(ctx);select{case <-ctx.Done():return;case <-ticker.C:} } }()
}

func (s *Service) replicaReadCandidates(ctx context.Context, x asset) []string {
	rows,err:=s.db.Query(ctx,`SELECT r.node_id FROM asset_replicas r JOIN storage_nodes n ON n.id=r.node_id WHERE r.asset_id=$1 AND r.state='READY' ORDER BY CASE WHEN r.node_id=$2 THEN 0 ELSE 1 END,r.verified_at DESC`,x.ID,x.NodeID)
	if err!=nil{return []string{x.NodeID}}
	defer rows.Close();ids:=[]string{}
	for rows.Next(){var id string;if rows.Scan(&id)==nil{ids=append(ids,id)}}
	return ids
}

func (s *Service) markReplicaUnavailable(ctx context.Context,assetID,nodeID,reason string) {
	_,_=s.db.Exec(ctx,`UPDATE asset_replicas SET state='MISSING',last_error=$3,updated_at=now() WHERE asset_id=$1 AND node_id=$2 AND state='READY'`,assetID,nodeID,reason)
	_,_=s.db.Exec(ctx,`UPDATE assets a SET storage_state=CASE WHEN (SELECT count(*) FROM asset_replicas r WHERE r.asset_id=a.id AND r.state='READY')=0 THEN 'FAILED' WHEN (SELECT count(*) FROM asset_replicas r WHERE r.asset_id=a.id AND r.state='READY')<a.replica_target THEN 'DEGRADED' ELSE 'AVAILABLE' END WHERE a.id=$1 AND a.status='ACTIVE'`,assetID)
	_,_=s.db.Exec(ctx,`INSERT INTO storage_jobs(id,asset_id,kind,source_node_id,state) SELECT $2,a.id,'REPLICATE',a.storage_node_id,'PENDING' FROM assets a WHERE a.id=$1 AND a.status='ACTIVE' AND EXISTS(SELECT 1 FROM asset_replicas r WHERE r.asset_id=a.id AND r.state='READY') AND (SELECT count(*) FROM asset_replicas r WHERE r.asset_id=a.id AND r.state='READY')<a.replica_target ON CONFLICT DO NOTHING`,assetID,newID())
	_,_=s.db.Exec(ctx,`INSERT INTO storage_jobs(id,asset_id,kind,source_node_id,state) SELECT $2,a.id,'DRAIN_REPLICATE',drain.node_id,'PENDING' FROM assets a CROSS JOIN LATERAL (SELECT r.node_id FROM asset_replicas r JOIN storage_nodes n ON n.id=r.node_id WHERE r.asset_id=a.id AND r.state='READY' AND n.status='DRAINING' LIMIT 1) drain WHERE a.id=$1 AND a.status='ACTIVE' AND NOT EXISTS(SELECT 1 FROM storage_jobs j WHERE j.asset_id=a.id AND j.kind='DRAIN_REPLICATE' AND j.state IN ('PENDING','RETRY','RUNNING')) ON CONFLICT DO NOTHING`,assetID,newID())
}

// Reconciliation verifies a bounded batch each cycle so large catalogs do not
// create an unbounded burst of node requests.
func (s *Service) reconcileReplicas(ctx context.Context) {
	rows,err:=s.db.Query(ctx,`SELECT a.id,a.storage_key,a.checksum,a.size_bytes,r.node_id,r.checksum_sha256,r.size_bytes FROM assets a JOIN asset_replicas r ON r.asset_id=a.id WHERE a.status='ACTIVE' AND r.state='READY' ORDER BY r.verified_at NULLS FIRST LIMIT 500`)
	if err!=nil{return}
	type item struct{id,key,checksum,node,replicaChecksum string;size,replicaSize int64};items:=[]item{}
	for rows.Next(){var x item;if rows.Scan(&x.id,&x.key,&x.checksum,&x.size,&x.node,&x.replicaChecksum,&x.replicaSize)==nil{items=append(items,x)}};rows.Close()
	for _,x:=range items {
		node,ok:=s.nodes[x.node];if !ok{s.markReplicaUnavailable(ctx,x.id,x.node,"node is not configured");continue}
		req,e:=http.NewRequestWithContext(ctx,http.MethodHead,node.URL+"/internal/v1/objects/"+x.key,nil);if e!=nil{continue};req.Header.Set("Authorization","Bearer "+node.Token)
		resp,e:=node.HTTP.Do(req);if e!=nil{s.markReplicaUnavailable(ctx,x.id,x.node,"node unavailable during reconciliation");continue}
		valid:=x.replicaChecksum==x.checksum&&x.replicaSize==x.size&&resp.StatusCode==http.StatusOK&&resp.Header.Get("X-Checksum-SHA256")==x.checksum&&resp.Header.Get("Content-Length")==fmt.Sprint(x.size);_=resp.Body.Close()
		if !valid{s.markReplicaUnavailable(ctx,x.id,x.node,"checksum or size mismatch during reconciliation");continue}
		_,_=s.db.Exec(ctx,`UPDATE asset_replicas SET verified_at=now(),last_error=NULL,updated_at=now() WHERE asset_id=$1 AND node_id=$2 AND state='READY'`,x.id,x.node)
	}
}

func (s *Service) healthyReplicaSource(ctx context.Context,assetID,checksum string,size int64,excludeID,preferredID string)(string,storageNodeClient,bool) {
	var none storageNodeClient
	var key string
	if err:=s.db.QueryRow(ctx,`SELECT storage_key FROM assets WHERE id=$1`,assetID).Scan(&key);err!=nil{return "",none,false}
	rows,err:=s.db.Query(ctx,`SELECT node_id FROM asset_replicas WHERE asset_id=$1 AND state='READY' AND node_id<>$2 ORDER BY CASE WHEN node_id=$3 THEN 0 ELSE 1 END,verified_at DESC`,assetID,excludeID,preferredID)
	if err!=nil{return "",none,false};defer rows.Close();ids:=[]string{};for rows.Next(){var id string;if rows.Scan(&id)==nil{ids=append(ids,id)}}
	for _,id:=range ids {
		node,ok:=s.nodes[id];if !ok{s.markReplicaUnavailable(ctx,assetID,id,"node is not configured as a replication source");continue};req,e:=http.NewRequestWithContext(ctx,http.MethodHead,node.URL+"/internal/v1/objects/"+key,nil);if e!=nil{continue};req.Header.Set("Authorization","Bearer "+node.Token)
		resp,e:=node.HTTP.Do(req);if e!=nil{s.markReplicaUnavailable(ctx,assetID,id,"node unavailable as replication source");continue}
		valid:=resp.StatusCode==http.StatusOK&&resp.Header.Get("X-Checksum-SHA256")==checksum&&resp.Header.Get("Content-Length")==fmt.Sprint(size);_=resp.Body.Close()
		if valid{return id,node,true};s.markReplicaUnavailable(ctx,assetID,id,"source checksum or size mismatch")
	}
	return "",none,false
}

func (s *Service) enqueueDeletion(ctx context.Context,assetID string) {
	if len(s.nodes)==0{return}
	_,_=s.db.Exec(ctx,`INSERT INTO storage_jobs(id,asset_id,kind,source_node_id,state) SELECT gen_random_uuid()::text,r.asset_id,'DELETE',r.node_id,'PENDING' FROM asset_replicas r JOIN assets a ON a.id=r.asset_id WHERE r.asset_id=$1 AND r.state='READY' AND a.status='DELETE_PENDING' AND NOT EXISTS(SELECT 1 FROM storage_jobs j WHERE j.asset_id=r.asset_id AND j.kind='DELETE' AND j.source_node_id=r.node_id AND j.state IN ('PENDING','RETRY','RUNNING','DONE')) ON CONFLICT DO NOTHING`,assetID)
}

func (s *Service) deleteOne(ctx context.Context) error {
	var jobID,assetID,nodeID,key,kind string
	_,_=s.db.Exec(ctx,`UPDATE storage_jobs SET state='RETRY',locked_at=NULL,available_at=now(),last_error='worker lease expired',updated_at=now() WHERE kind IN ('DELETE','DRAIN_DELETE') AND state='RUNNING' AND locked_at<now()-interval '10 minutes'`)
	rows,e:=s.db.Query(ctx,`SELECT id FROM assets WHERE status='DELETE_PENDING' AND storage_node_id IS NOT NULL ORDER BY delete_requested_at LIMIT 200`)
	if e==nil {ids:=[]string{};for rows.Next(){var id string;if rows.Scan(&id)==nil{ids=append(ids,id)}};rows.Close();for _,id:=range ids{s.enqueueDeletion(ctx,id)}}
	tx,err:=s.db.Begin(ctx);if err!=nil{return err};defer tx.Rollback(ctx)
	err=tx.QueryRow(ctx,`WITH candidate AS (SELECT j.id FROM storage_jobs j JOIN assets a ON a.id=j.asset_id WHERE j.kind IN ('DELETE','DRAIN_DELETE') AND j.state IN ('PENDING','RETRY') AND j.available_at<=now() AND ((j.kind='DELETE' AND a.status='DELETE_PENDING') OR (j.kind='DRAIN_DELETE' AND a.status='ACTIVE' AND EXISTS(SELECT 1 FROM asset_replicas old JOIN storage_nodes oldn ON oldn.id=old.node_id JOIN asset_replicas replacement ON replacement.asset_id=old.asset_id JOIN storage_nodes newn ON newn.id=replacement.node_id WHERE old.asset_id=a.id AND old.node_id=j.source_node_id AND old.state='READY' AND replacement.state='READY' AND replacement.node_id<>old.node_id AND newn.failure_domain<>oldn.failure_domain AND replacement.checksum_sha256=a.checksum AND replacement.size_bytes=a.size_bytes))) ORDER BY j.available_at,j.created_at LIMIT 1 FOR UPDATE OF a,j SKIP LOCKED) UPDATE storage_jobs j SET state='RUNNING',attempts=attempts+1,locked_at=now(),updated_at=now() FROM candidate c,assets a WHERE j.id=c.id AND a.id=j.asset_id RETURNING j.id,j.asset_id,COALESCE(j.source_node_id,''),a.storage_key,j.kind`).Scan(&jobID,&assetID,&nodeID,&key,&kind)
	if err!=nil{return err};if err=tx.Commit(ctx);err!=nil{return err}
	if kind=="DRAIN_DELETE" { return s.finishDrainDelete(ctx,jobID,assetID,nodeID,key) }
	node,ok:=s.nodes[nodeID];if !ok{s.retryDeleteJob(ctx,jobID,"storage node is not configured");return nil}
	req,err:=http.NewRequestWithContext(ctx,http.MethodDelete,node.URL+"/internal/v1/objects/"+key,nil);if err!=nil{s.retryDeleteJob(ctx,jobID,"invalid delete request");return nil};req.Header.Set("Authorization","Bearer "+node.Token)
	resp,err:=node.HTTP.Do(req);if err!=nil{s.retryDeleteJob(ctx,jobID,"storage node unavailable during deletion");return nil};_=resp.Body.Close();if resp.StatusCode!=http.StatusNoContent&&resp.StatusCode!=http.StatusNotFound{s.retryDeleteJob(ctx,jobID,"storage node rejected deletion");return nil}
	if _,err=s.db.Exec(ctx,`UPDATE asset_replicas SET state='DELETED',updated_at=now(),last_error=NULL WHERE asset_id=$1 AND node_id=$2`,assetID,nodeID);err!=nil{return err}
	if _,err=s.db.Exec(ctx,`UPDATE storage_jobs SET state='DONE',locked_at=NULL,last_error=NULL,updated_at=now() WHERE id=$1`,jobID);err!=nil{return err}
	_,err=s.db.Exec(ctx,`UPDATE assets SET status='DELETED' WHERE id=$1 AND status='DELETE_PENDING' AND EXISTS(SELECT 1 FROM storage_jobs WHERE asset_id=$1 AND kind='DELETE') AND NOT EXISTS(SELECT 1 FROM storage_jobs WHERE asset_id=$1 AND kind='DELETE' AND state<>'DONE')`,assetID)
	return err
}

func (s *Service) finishDrainDelete(ctx context.Context,jobID,assetID,nodeID,key string) error {
	node,ok:=s.nodes[nodeID];if !ok{s.retryDeleteJob(ctx,jobID,"storage node is not configured");return nil}
	tx,err:=s.db.Begin(ctx);if err!=nil{return err};defer tx.Rollback(ctx)
	var status string;if err=tx.QueryRow(ctx,`SELECT status FROM assets WHERE id=$1 FOR UPDATE`,assetID).Scan(&status);err!=nil{return err};if status!="ACTIVE"{_,_=tx.Exec(ctx,`UPDATE storage_jobs SET state='CANCELLED',locked_at=NULL,updated_at=now() WHERE id=$1`,jobID);return tx.Commit(ctx)}
	var nodeStatus string;if err=tx.QueryRow(ctx,`SELECT status FROM storage_nodes WHERE id=$1 FOR SHARE`,nodeID).Scan(&nodeStatus);err!=nil{return err};if nodeStatus!="DRAINING"{_,_=tx.Exec(ctx,`UPDATE storage_jobs SET state='CANCELLED',locked_at=NULL,updated_at=now() WHERE id=$1`,jobID);return tx.Commit(ctx)}
	var replacementID,checksum string;var size int64
	err=tx.QueryRow(ctx,`SELECT r.node_id,a.checksum,a.size_bytes FROM asset_replicas r JOIN assets a ON a.id=r.asset_id JOIN storage_nodes target ON target.id=r.node_id JOIN storage_nodes source ON source.id=$2 WHERE r.asset_id=$1 AND r.node_id<>$2 AND r.state='READY' AND target.failure_domain<>source.failure_domain AND r.checksum_sha256=a.checksum AND r.size_bytes=a.size_bytes ORDER BY r.verified_at DESC NULLS LAST LIMIT 1 FOR UPDATE OF r`,assetID,nodeID).Scan(&replacementID,&checksum,&size)
	if err!=nil{s.retryDeleteJob(ctx,jobID,"verified replacement is no longer available");return err}
	replacement,ok:=s.nodes[replacementID];if !ok{s.retryDeleteJob(ctx,jobID,"replacement node is not configured");return nil}
	verify,err:=http.NewRequestWithContext(ctx,http.MethodHead,replacement.URL+"/internal/v1/objects/"+key,nil);if err!=nil{s.retryDeleteJob(ctx,jobID,"replacement verification request failed");return nil};verify.Header.Set("Authorization","Bearer "+replacement.Token);verified,err:=replacement.HTTP.Do(verify);if err!=nil{s.retryDeleteJob(ctx,jobID,"replacement node unavailable");return nil};valid:=verified.StatusCode==http.StatusOK&&verified.Header.Get("X-Checksum-SHA256")==checksum&&verified.Header.Get("Content-Length")==fmt.Sprint(size);_=verified.Body.Close();if !valid{_,_=tx.Exec(ctx,`UPDATE asset_replicas SET state='MISSING',last_error='replacement failed verification before node drain',updated_at=now() WHERE asset_id=$1 AND node_id=$2`,assetID,replacementID);_,_=tx.Exec(ctx,`UPDATE assets SET storage_state=CASE WHEN (SELECT count(*) FROM asset_replicas WHERE asset_id=$1 AND state='READY')=0 THEN 'FAILED' WHEN (SELECT count(*) FROM asset_replicas WHERE asset_id=$1 AND state='READY')<replica_target THEN 'DEGRADED' ELSE 'AVAILABLE' END WHERE id=$1`,assetID);if e:=tx.Commit(ctx);e!=nil{return e};s.retryDeleteJob(ctx,jobID,"replacement failed checksum verification");return nil}
	remove,err:=http.NewRequestWithContext(ctx,http.MethodDelete,node.URL+"/internal/v1/objects/"+key,nil);if err!=nil{s.retryDeleteJob(ctx,jobID,"invalid source delete request");return nil};remove.Header.Set("Authorization","Bearer "+node.Token);removed,err:=node.HTTP.Do(remove);if err!=nil{s.retryDeleteJob(ctx,jobID,"draining node unavailable");return nil};_=removed.Body.Close();if removed.StatusCode!=http.StatusNoContent&&removed.StatusCode!=http.StatusNotFound{s.retryDeleteJob(ctx,jobID,"draining node rejected deletion");return nil}
	if _,err=tx.Exec(ctx,`UPDATE asset_replicas SET state='DRAINED',updated_at=now(),last_error=NULL WHERE asset_id=$1 AND node_id=$2`,assetID,nodeID);err!=nil{return err};if _,err=tx.Exec(ctx,`UPDATE storage_jobs SET state='DONE',locked_at=NULL,last_error=NULL,updated_at=now() WHERE id=$1`,jobID);err!=nil{return err};if _,err=tx.Exec(ctx,`UPDATE assets SET storage_state=CASE WHEN (SELECT count(*) FROM asset_replicas WHERE asset_id=$1 AND state='READY')>=replica_target THEN 'AVAILABLE' ELSE 'DEGRADED' END WHERE id=$1 AND status='ACTIVE'`,assetID);err!=nil{return err};return tx.Commit(ctx)
}

func (s *Service) healthyDrainReplacement(ctx context.Context,assetID,sourceID string)(string,storageNodeClient,bool) {
	var none storageNodeClient
	var checksum string;var size int64
	if err:=s.db.QueryRow(ctx,`SELECT checksum,size_bytes FROM assets WHERE id=$1`,assetID).Scan(&checksum,&size);err!=nil{return "",none,false}
	rows,err:=s.db.Query(ctx,`SELECT r.node_id FROM asset_replicas r JOIN storage_nodes target ON target.id=r.node_id JOIN storage_nodes source ON source.id=$2 WHERE r.asset_id=$1 AND r.node_id<>$2 AND r.state='READY' AND target.failure_domain<>source.failure_domain AND r.checksum_sha256=$3 AND r.size_bytes=$4 ORDER BY r.verified_at DESC NULLS LAST`,assetID,sourceID,checksum,size)
	if err!=nil{return "",none,false};defer rows.Close();ids:=[]string{};for rows.Next(){var id string;if rows.Scan(&id)==nil{ids=append(ids,id)}}
	var key string;if err=s.db.QueryRow(ctx,`SELECT storage_key FROM assets WHERE id=$1`,assetID).Scan(&key);err!=nil{return "",none,false}
	for _,id:=range ids { node,ok:=s.nodes[id];if !ok{continue};req,e:=http.NewRequestWithContext(ctx,http.MethodHead,node.URL+"/internal/v1/objects/"+key,nil);if e!=nil{continue};req.Header.Set("Authorization","Bearer "+node.Token);resp,e:=node.HTTP.Do(req);if e!=nil{continue};valid:=resp.StatusCode==http.StatusOK&&resp.Header.Get("X-Checksum-SHA256")==checksum&&resp.Header.Get("Content-Length")==fmt.Sprint(size);_=resp.Body.Close();if valid{return id,node,true};s.markReplicaUnavailable(ctx,assetID,id,"replacement failed verification before node drain") }
	return "",none,false
}

func (s *Service) retryDeleteJob(ctx context.Context,id,msg string) {
	_,_=s.db.Exec(ctx,`UPDATE storage_jobs SET state=CASE WHEN attempts>=8 THEN 'FAILED' ELSE 'RETRY' END,available_at=now()+make_interval(secs=>LEAST(3600,power(2,LEAST(attempts,10))::int)),locked_at=NULL,last_error=$2,updated_at=now() WHERE id=$1`,id,msg)
}

func (s *Service) heartbeatNodes(ctx context.Context) {
	for _,n:=range s.nodes {
		ok:=false
		for _,endpoint:=range []string{"/internal/v1/health","/internal/v1/capacity"} {
			req,err:=http.NewRequestWithContext(ctx,http.MethodGet,n.URL+endpoint,nil);if err!=nil{break};req.Header.Set("Authorization","Bearer "+n.Token)
			resp,err:=n.HTTP.Do(req);if err!=nil{break}
			if endpoint=="/internal/v1/capacity" && resp.StatusCode==http.StatusOK {
				var cap struct{Capacity int64 `json:"capacity_bytes"`;Used int64 `json:"used_bytes"`;Available int64 `json:"available_bytes"`;Reserved int64 `json:"reserved_bytes"`}
				if json.NewDecoder(resp.Body).Decode(&cap)==nil && cap.Capacity>0 { _,err=s.db.Exec(ctx,`UPDATE storage_nodes SET status=CASE WHEN status='DRAINING' THEN status ELSE 'ACTIVE' END,capacity_bytes=$2,used_bytes=$3,last_heartbeat_at=now() WHERE id=$1`,n.ID,cap.Capacity,cap.Used);ok=err==nil }
			} else if endpoint=="/internal/v1/health" { ok=resp.StatusCode==http.StatusOK }
			_ = resp.Body.Close();if !ok {break}
		}
		_ = ok // stale heartbeat age excludes unavailable nodes from placement and is exposed as OFFLINE to operators.
	}
}

func (s *Service) reserveStorageNode(ctx context.Context,size int64,excludeDomains []string,jobID string) (storageNodeClient,error) {
	var none storageNodeClient
	ids:=make([]string,0,len(s.nodes));for id:=range s.nodes{ids=append(ids,id)};if excludeDomains==nil{excludeDomains=[]string{}}
	tx,err:=s.db.Begin(ctx);if err!=nil{return none,err};defer tx.Rollback(ctx)
	rows,err:=tx.Query(ctx,`SELECT id,failure_domain FROM storage_nodes WHERE id=ANY($1) AND status='ACTIVE' AND last_heartbeat_at>now()-interval '90 seconds' AND capacity_bytes-used_bytes-reserved_bytes-$2>=GREATEST(0,$3) AND NOT(failure_domain=ANY($4)) ORDER BY (used_bytes+reserved_bytes)::numeric/NULLIF(capacity_bytes,0),capacity_bytes DESC FOR UPDATE SKIP LOCKED`,ids,size,parseEnvInt64("CAS_NODE_MIN_FREE_BYTES",1048576),excludeDomains)
	if err!=nil{return none,err};defer rows.Close()
	var id,domain string
	if !rows.Next(){return none,fmt.Errorf("no storage node has available capacity")};if err=rows.Scan(&id,&domain);err!=nil{return none,err};rows.Close()
	if _,err=tx.Exec(ctx,`UPDATE storage_nodes SET reserved_bytes=reserved_bytes+$2 WHERE id=$1`,id,size);err!=nil{return none,err}
	if jobID!="" { if _,err=tx.Exec(ctx,`UPDATE storage_jobs SET target_node_id=$2 WHERE id=$1`,jobID,id);err!=nil{return none,err} }
	if err=tx.Commit(ctx);err!=nil{return none,err}
	return s.nodes[id],nil
}

func (s *Service) releaseReservation(ctx context.Context,nodeID string,size int64) { _,_=s.db.Exec(ctx,`UPDATE storage_nodes SET reserved_bytes=GREATEST(0,reserved_bytes-$2) WHERE id=$1`,nodeID,size) }
func (s *Service) commitReservation(ctx context.Context,nodeID string,size int64) { _,_=s.db.Exec(ctx,`UPDATE storage_nodes SET reserved_bytes=GREATEST(0,reserved_bytes-$2),used_bytes=used_bytes+$2 WHERE id=$1`,nodeID,size) }

func (s *Service) saveClusterAsset(ctx context.Context,id,appID,name,mime,checksum,key string,size int64,metadata any,nodeID string,target int) error {
	state:="AVAILABLE";if target>1{state="DEGRADED"}
	tx,err:=s.db.Begin(ctx);if err!=nil{return err};defer tx.Rollback(ctx)
	if _,err=tx.Exec(ctx,`INSERT INTO assets(id,owner_application_id,original_filename,mime_type,size_bytes,checksum,storage_key,metadata,storage_node_id,replica_target,storage_state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,id,appID,name,mime,size,checksum,key,metadata,nodeID,target,state);err!=nil{return err}
	if _,err=tx.Exec(ctx,`INSERT INTO asset_replicas(asset_id,node_id,state,checksum_sha256,size_bytes,verified_at) VALUES($1,$2,'READY',$3,$4,now())`,id,nodeID,checksum,size);err!=nil{return err}
	if target>1 { if _,err=tx.Exec(ctx,`INSERT INTO storage_jobs(id,asset_id,kind,source_node_id,state) VALUES($1,$2,'REPLICATE',$3,'PENDING')`,newID(),id,nodeID);err!=nil{return err} }
	return tx.Commit(ctx)
}

func (s *Service) refreshReplicaPolicy(ctx context.Context,appID string,target int) error {
	if _,err:=s.db.Exec(ctx,`UPDATE assets a SET replica_target=$2,storage_state=CASE WHEN (SELECT count(*) FROM asset_replicas r WHERE r.asset_id=a.id AND r.state='READY') >= $2 THEN 'AVAILABLE' ELSE 'DEGRADED' END WHERE owner_application_id=$1 AND storage_node_id IS NOT NULL`,appID,target);err!=nil{return err}
	_,err:=s.db.Exec(ctx,`INSERT INTO storage_jobs(id,asset_id,kind,source_node_id,state) SELECT gen_random_uuid()::text,a.id,'REPLICATE',a.storage_node_id,'PENDING' FROM assets a WHERE a.owner_application_id=$1 AND a.storage_node_id IS NOT NULL AND (SELECT count(*) FROM asset_replicas r WHERE r.asset_id=a.id AND r.state='READY')<a.replica_target AND NOT EXISTS(SELECT 1 FROM storage_jobs j WHERE j.asset_id=a.id AND j.kind='REPLICATE' AND j.state IN ('PENDING','RETRY','RUNNING')) ON CONFLICT DO NOTHING`,appID)
	return err
}

func parseEnvInt64(name string,fallback int64) int64 { v:=strings.TrimSpace(os.Getenv(name));if v==""{return fallback};n,err:=strconv.ParseInt(v,10,64);if err!=nil||n<0{return fallback};return n }

func (s *Service) replicateOne(ctx context.Context) error {
	s.recoverExpiredJobs(ctx)
	var jobID,assetID,sourceID,key string;var target *string;var checksum string;var size int64
	tx,err:=s.db.Begin(ctx);if err!=nil{return err};defer tx.Rollback(ctx)
	err=tx.QueryRow(ctx,`WITH candidate AS (SELECT j.id FROM storage_jobs j JOIN assets a ON a.id=j.asset_id WHERE j.kind IN ('REPLICATE','DRAIN_REPLICATE') AND j.state IN ('PENDING','RETRY') AND j.available_at<=now() AND a.status='ACTIVE' ORDER BY j.available_at,j.created_at LIMIT 1 FOR UPDATE OF a,j SKIP LOCKED) UPDATE storage_jobs j SET state='RUNNING',attempts=attempts+1,locked_at=now(),updated_at=now() FROM candidate c,assets a WHERE j.id=c.id AND a.id=j.asset_id RETURNING j.id,j.asset_id,COALESCE(j.source_node_id,''),j.target_node_id,a.checksum,a.size_bytes,a.storage_key`).Scan(&jobID,&assetID,&sourceID,&target,&checksum,&size,&key)
	if err!=nil{return err};if err=tx.Commit(ctx);err!=nil{return err}
	if target!=nil { if _,ok:=s.nodes[*target];!ok {target=nil} }
	if target==nil {
		var domains []string;rows,e:=s.db.Query(ctx,`SELECT n.failure_domain FROM asset_replicas r JOIN storage_nodes n ON n.id=r.node_id WHERE r.asset_id=$1`,assetID);if e==nil{for rows.Next(){var d string;if rows.Scan(&d)==nil{domains=append(domains,d)}};rows.Close()}
		dest,e:=s.reserveStorageNode(ctx,size,domains,jobID);if e!=nil {s.retryJob(ctx,jobID,e.Error());return nil};target=&dest.ID
	}
	srcID,src,ok:=s.healthyReplicaSource(ctx,assetID,checksum,size,*target,sourceID);if !ok {s.retryJob(ctx,jobID,"no verified source replica is available");return nil};_ = srcID;dst,ok:=s.nodes[*target];if !ok {s.retryJob(ctx,jobID,"target node is not configured");return nil}
	get,err:=http.NewRequestWithContext(ctx,http.MethodGet,src.URL+"/internal/v1/objects/"+key,nil);if err!=nil{s.retryJob(ctx,jobID,err.Error());return nil};get.Header.Set("Authorization","Bearer "+src.Token)
	resp,err:=src.HTTP.Do(get);if err!=nil{s.retryJob(ctx,jobID,"source node unavailable");return nil};if resp.StatusCode!=http.StatusOK||resp.Header.Get("X-Checksum-SHA256")!=checksum||resp.Header.Get("Content-Length")!=fmt.Sprint(size) { _=resp.Body.Close();s.retryJob(ctx,jobID,"source object failed checksum verification");return nil }
	put,err:=http.NewRequestWithContext(ctx,http.MethodPut,dst.URL+"/internal/v1/objects/"+key,resp.Body);if err!=nil{_=resp.Body.Close();s.retryJob(ctx,jobID,err.Error());return nil};put.ContentLength=size;put.Header.Set("Authorization","Bearer "+dst.Token);put.Header.Set("X-Checksum-SHA256",checksum);put.Header.Set("X-Object-Size",fmt.Sprint(size))
	copyResp,err:=dst.HTTP.Do(put);_=resp.Body.Close();if err!=nil{s.retryJob(ctx,jobID,"target node unavailable");return nil};_=copyResp.Body.Close();if copyResp.StatusCode<200||copyResp.StatusCode>=300{s.retryJob(ctx,jobID,"target node rejected replica");return nil}
	verifyReq,err:=http.NewRequestWithContext(ctx,http.MethodHead,dst.URL+"/internal/v1/objects/"+key,nil);if err!=nil{s.retryJob(ctx,jobID,"replica verification request failed");return nil};verifyReq.Header.Set("Authorization","Bearer "+dst.Token);verifyResp,err:=dst.HTTP.Do(verifyReq);if err!=nil{s.retryJob(ctx,jobID,"target node unavailable during replica verification");return nil};verified:=verifyResp.StatusCode==http.StatusOK&&verifyResp.Header.Get("X-Checksum-SHA256")==checksum&&verifyResp.Header.Get("Content-Length")==fmt.Sprint(size);_=verifyResp.Body.Close();if !verified{s.retryJob(ctx,jobID,"target replica checksum verification failed");return nil}
	_,err=s.db.Exec(ctx,`INSERT INTO asset_replicas(asset_id,node_id,state,checksum_sha256,size_bytes,verified_at) VALUES($1,$2,'READY',$3,$4,now()) ON CONFLICT(asset_id,node_id) DO UPDATE SET state='READY',checksum_sha256=EXCLUDED.checksum_sha256,size_bytes=EXCLUDED.size_bytes,verified_at=now(),last_error=NULL,updated_at=now()`,assetID,*target,checksum,size);if err!=nil{return err}
	s.commitReservation(ctx,*target,size)
	_,err=s.db.Exec(ctx,`UPDATE storage_jobs SET state='DONE',target_node_id=$2,locked_at=NULL,updated_at=now(),last_error=NULL WHERE id=$1`,jobID,*target);if err!=nil{return err}
	var ready,targetCount int
	if err=s.db.QueryRow(ctx,`SELECT count(*) FILTER (WHERE state='READY'),max(a.replica_target) FROM asset_replicas r JOIN assets a ON a.id=r.asset_id WHERE r.asset_id=$1`,assetID).Scan(&ready,&targetCount);err!=nil{return err}
	state:="DEGRADED";if ready>=targetCount{state="AVAILABLE"}
	if _,err=s.db.Exec(ctx,`UPDATE assets SET storage_state=$2 WHERE id=$1`,assetID,state);err!=nil{return err}
	if ready<targetCount { _,err=s.db.Exec(ctx,`INSERT INTO storage_jobs(id,asset_id,kind,source_node_id,state) SELECT $2,id,'REPLICATE',storage_node_id,'PENDING' FROM assets WHERE id=$1 ON CONFLICT DO NOTHING`,assetID,newID()) }
	if sourceID!="" { var draining bool;_ = s.db.QueryRow(ctx,`SELECT status='DRAINING' FROM storage_nodes WHERE id=$1`,sourceID).Scan(&draining);if draining { _,e:=s.db.Exec(ctx,`INSERT INTO storage_jobs(id,asset_id,kind,source_node_id,state) SELECT $1,$2,'DRAIN_DELETE',$3,'PENDING' WHERE EXISTS(SELECT 1 FROM asset_replicas replacement JOIN storage_nodes rn ON rn.id=replacement.node_id JOIN storage_nodes oldn ON oldn.id=$3 WHERE replacement.asset_id=$2 AND replacement.node_id=$4 AND replacement.state='READY' AND rn.failure_domain<>oldn.failure_domain AND replacement.checksum_sha256=$5 AND replacement.size_bytes=$6) AND NOT EXISTS(SELECT 1 FROM storage_jobs j WHERE j.asset_id=$2 AND j.kind='DRAIN_DELETE' AND j.source_node_id=$3 AND j.state IN ('PENDING','RETRY','RUNNING')) ON CONFLICT DO NOTHING`,newID(),assetID,sourceID,*target,checksum,size);if e!=nil{return e} } }
	return err
}

func (s *Service) retryJob(ctx context.Context,id,msg string) {
	_,_=s.db.Exec(ctx,`WITH current_job AS (SELECT id,asset_id,target_node_id FROM storage_jobs WHERE id=$1 FOR UPDATE), released AS (UPDATE storage_nodes n SET reserved_bytes=GREATEST(0,n.reserved_bytes-a.size_bytes) FROM current_job j JOIN assets a ON a.id=j.asset_id WHERE n.id=j.target_node_id RETURNING n.id) UPDATE storage_jobs j SET state=CASE WHEN j.attempts>=8 THEN 'FAILED' ELSE 'RETRY' END,available_at=now()+make_interval(secs=>LEAST(3600,power(2,LEAST(j.attempts,10))::int)),target_node_id=NULL,locked_at=NULL,last_error=$2,updated_at=now() FROM current_job c WHERE j.id=c.id`,id,msg)
}

func (s *Service) recoverExpiredJobs(ctx context.Context) {
	_,_=s.db.Exec(ctx,`WITH expired AS (SELECT id,asset_id,target_node_id FROM storage_jobs WHERE state='RUNNING' AND locked_at<now()-interval '10 minutes' FOR UPDATE SKIP LOCKED), released AS (UPDATE storage_nodes n SET reserved_bytes=GREATEST(0,n.reserved_bytes-a.size_bytes) FROM expired e JOIN assets a ON a.id=e.asset_id WHERE n.id=e.target_node_id RETURNING n.id) UPDATE storage_jobs j SET state='RETRY',target_node_id=NULL,locked_at=NULL,available_at=now(),last_error='worker lease expired',updated_at=now() FROM expired e WHERE j.id=e.id`)
}

func (s *Service) adminStorageNodes(w http.ResponseWriter,r *http.Request) {
	rows,err:=s.db.Query(r.Context(),`SELECT n.id,n.name,n.base_url,n.failure_domain,CASE WHEN n.status='DRAINING' THEN n.status WHEN n.last_heartbeat_at<now()-interval '90 seconds' THEN 'OFFLINE' ELSE n.status END,n.capacity_bytes,n.used_bytes,n.reserved_bytes,n.last_heartbeat_at,(SELECT count(*) FROM asset_replicas r WHERE r.node_id=n.id AND r.state='READY'),(SELECT count(*) FROM storage_jobs j WHERE j.source_node_id=n.id AND j.kind IN ('DRAIN_REPLICATE','DRAIN_DELETE') AND j.state IN ('PENDING','RETRY','RUNNING')),(SELECT count(*) FROM storage_jobs j WHERE j.source_node_id=n.id AND j.kind IN ('DRAIN_REPLICATE','DRAIN_DELETE') AND j.state='FAILED') FROM storage_nodes n ORDER BY n.name`)
	if err!=nil{fail(w,500,"storage nodes unavailable");return};defer rows.Close();out:=[]map[string]any{}
	for rows.Next(){var id,name,baseURL,domain,status string;var capacity,used,reserved int64;var heartbeat *time.Time;var replicas,drainPending,drainFailed int;if rows.Scan(&id,&name,&baseURL,&domain,&status,&capacity,&used,&reserved,&heartbeat,&replicas,&drainPending,&drainFailed)==nil{out=append(out,map[string]any{"node_id":id,"name":name,"base_url":baseURL,"failure_domain":domain,"status":status,"capacity_bytes":capacity,"used_bytes":used,"reserved_bytes":reserved,"ready_replicas":replicas,"drain_jobs_pending":drainPending,"drain_jobs_failed":drainFailed,"last_heartbeat_at":heartbeat})}}
	writeJSON(w,200,map[string]any{"data":out})
}

func (s *Service) adminStorageNode(w http.ResponseWriter,r *http.Request) {
	if r.Method!=http.MethodPatch{fail(w,405,"method not allowed");return};id:=strings.TrimPrefix(r.URL.Path,"/api/v1/admin/storage/nodes/");if id==""||strings.Contains(id,"/"){fail(w,404,"storage node not found");return}
	var in struct{Status string `json:"status"`};if decodeJSON(r,&in)!=nil||(in.Status!="ACTIVE"&&in.Status!="DRAINING"){fail(w,422,"status must be ACTIVE or DRAINING");return}
	tag,err:=s.db.Exec(r.Context(),`UPDATE storage_nodes SET status=$2 WHERE id=$1`,id,in.Status);if err!=nil||tag.RowsAffected()!=1{fail(w,404,"storage node not found");return};if in.Status=="DRAINING"{_,_=s.db.Exec(r.Context(),`UPDATE storage_jobs SET state='RETRY',available_at=now(),last_error=NULL,updated_at=now() WHERE kind='DRAIN_DELETE' AND source_node_id=$1 AND state='FAILED'`,id);_,err=s.db.Exec(r.Context(),`INSERT INTO storage_jobs(id,asset_id,kind,source_node_id,state) SELECT gen_random_uuid()::text,r.asset_id,'DRAIN_REPLICATE',$1,'PENDING' FROM asset_replicas r JOIN assets a ON a.id=r.asset_id WHERE r.node_id=$1 AND r.state='READY' AND a.status='ACTIVE' AND NOT EXISTS(SELECT 1 FROM storage_jobs j WHERE j.asset_id=r.asset_id AND j.kind='DRAIN_REPLICATE' AND j.state IN ('PENDING','RETRY','RUNNING')) ON CONFLICT DO NOTHING`,id);if err!=nil{fail(w,500,"drain jobs could not be queued");return}};writeJSON(w,200,map[string]string{"node_id":id,"status":in.Status})
}

func (s *Service) adminStorageJobs(w http.ResponseWriter,r *http.Request) {
	rows,err:=s.db.Query(r.Context(),`SELECT id,asset_id,kind,COALESCE(source_node_id,''),COALESCE(target_node_id,''),state,attempts,available_at,last_error FROM storage_jobs ORDER BY created_at DESC LIMIT 200`)
	if err!=nil{fail(w,500,"storage jobs unavailable");return};defer rows.Close();out:=[]map[string]any{}
	for rows.Next(){var id,assetID,kind,source,target,state string;var attempts int;var available time.Time;var lastError *string;if rows.Scan(&id,&assetID,&kind,&source,&target,&state,&attempts,&available,&lastError)==nil{out=append(out,map[string]any{"job_id":id,"asset_id":assetID,"kind":kind,"source_node_id":source,"target_node_id":target,"state":state,"attempts":attempts,"available_at":available,"last_error":lastError})}}
	writeJSON(w,200,map[string]any{"data":out})
}

