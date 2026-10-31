# CAS storage operations runbook

This runbook covers the storage cluster added in CAS V2. It is an operator guide; it does not replace an environment-specific backup retention or incident-response policy.

## Dashboard and alerts

The admin API is the current operational dashboard surface:

- `GET /api/v1/admin/storage/nodes`: alert on `OFFLINE`; while a node is `DRAINING`, keep it online until `ready_replicas` and `drain_jobs_pending` both reach zero. Investigate any `drain_jobs_failed` value above zero.
- `GET /api/v1/admin/storage/jobs`: alert on `FAILED` jobs. `REPLICATE`/`DRAIN_REPLICATE` failures indicate degraded redundancy; `DELETE` failures mean physical deletion is incomplete.
- `GET /api/v1/admin/assets`: alert on assets with `storage_status=DEGRADED` for longer than the deployment's recovery objective.
- `GET /healthz`: checks the CAS process. It does not replace the storage-node checks above.

Poll these endpoints from the deployment's existing monitoring system. Keep node tokens, application keys, file names, and object contents out of monitoring labels and logs. Node heartbeat expires after 90 seconds; heartbeat polling runs every 20 seconds. Replica reconciliation checks at most 500 ready replicas every five minutes.

## Draining a node

1. Set the node to `DRAINING` with `PATCH /api/v1/admin/storage/nodes/{id}` and body `{"status":"DRAINING"}`. New placements stop and `DRAIN_REPLICATE` jobs are queued for its ready replicas.
2. Follow the node and job endpoints above. CAS verifies each replacement's checksum and size and requires a different failure domain before queuing `DRAIN_DELETE`. The worker verifies the replacement again immediately before deleting the source object, then marks its replica `DRAINED`.
3. Shut down the node only after `ready_replicas=0`, `drain_jobs_pending=0`, and `drain_jobs_failed=0`. If a job failed, restore node health/capacity or add an eligible node, then set the node to `DRAINING` again to enqueue any remaining ready replicas.
4. Once all three counts reach zero, object files managed by CAS have been removed from the drained node. Preserve backups according to the organization's retention policy; wipe the full volume only when secure-erasure procedures authorize it. To return a node to placement, set it back to `ACTIVE`.

## TLS and service-token rotation

- Monitor node certificate expiry and alert at least 30 days before expiration. Keep the CA private key offline; distribute only the CA certificate to CAS.
- For CA rotation, configure CAS to trust both old and new CA certificates, deploy node certificates signed by the new CA, confirm node heartbeats, then remove the old CA from CAS configuration.
- Rotate a node service token by preparing the new secret in the secret manager, updating the node and CAS configuration in a coordinated maintenance window, restarting both sides, and confirming the node heartbeat. Never place the token in a URL or browser configuration.
- CAS validates HTTPS certificates using system roots plus the configured CA file. Expired, hostname-mismatched, or untrusted certificates cause node health/read/write operations to fail and should raise an alert.

`CAS_NODE_MAX_OBJECT_BYTES` defaults to 10 GiB per node request. `CAS_MAX_UPLOAD_BYTES` defaults to 50 MiB at the CAS application API. `CAS_STREAM_TIMEOUT_SECONDS` defaults to 900 seconds and applies to CAS HTTP request/response streaming; set a suitable positive value on both CAS and the reverse proxy for the deployment's maximum transfer duration.

## Staging fault drills

Run these checks in a disposable staging cluster before production rollout. Keep a verified backup and avoid filling a shared filesystem.

- Upload a file above the configured CAS upload limit and above the storage-node object limit; CAS/node must reject it without leaving a partial object or reservation.
- Submit concurrent uploads whose combined size approaches a node's free space; reservations must prevent over-placement, and a full node must be excluded from new placements.
- Use a wrong/expired certificate and an invalid node token; node heartbeats must fail, the node must become visibly offline, and it must not receive new placements.
- Stop one node while reading an asset with another `READY` replica; download should fail over and retain the original SHA-256. Reconciliation should mark the missing copy and queue repair.
- Interrupt CAS-to-node networking during a replica transfer; the job should retry without declaring the destination `READY`. Restore the network and confirm the retry completes.
- Restart CAS while a worker job is `RUNNING`; after its 10-minute lease expires it should be retried, reservations released, and idempotent transfer should recover.
- Request deletion with active references and confirm it remains `DELETE_BLOCKED`. Deactivate the final reference and confirm node delete jobs complete before asset status becomes `DELETED`.

Capture the job state, node state, and checksums from each drill in the release record. The repository change prepares these controls and procedures; the drills must be executed in the target staging environment because its disks, certificates, and network policies are deployment-specific.

## Consistent backup

The database and every object volume must represent the same point in time. Do not rely on online replication as a backup.

1. Stop writes and workers by stopping every CAS instance and storage-node service. Leave PostgreSQL running for the database dump.
2. Create a PostgreSQL custom-format dump, for example:

   ```sh
   docker compose exec -T db pg_dump -U cas -Fc cas > cas-database.dump
   ```

3. Archive the CAS object volume and each storage-node volume separately. Find the actual Docker volume names with `docker volume ls`; Compose prefixes them with the project name.

   ```sh
   docker run --rm -v <project>_cas_data:/data:ro alpine \
     tar czf - -C /data . > cas-objects.tar.gz
   ```

   For a multi-node deployment, repeat for each node volume and label the archive with the node ID. Encrypt backups, store them outside the cluster, and apply the organization's access and retention controls.
4. Restart CAS and storage nodes, then confirm health, node heartbeats, and replication status.

## Restore rehearsal

Restore into an isolated Compose project with a separate database and fresh named volumes. Never restore a rehearsal archive over production volumes.

1. Start only the isolated PostgreSQL service and wait for its health check.
2. Restore the database dump to that isolated database with `pg_restore`; extract the matching CAS object archive and each storage-node archive into their corresponding isolated volumes.
3. Start the isolated CAS and storage nodes with the restored database and node volumes. Confirm `/healthz`, node heartbeats, asset metadata, and that representative assets can be downloaded with the expected SHA-256 checksum.
4. Confirm degraded or missing replicas are visible and repair jobs recover them. Record restore duration and any manual steps, then destroy the isolated rehearsal environment using its own Compose project and volume names.

Record the rehearsal date, archive identifiers, checksum verification results, and elapsed recovery time. A backup is not considered verified until this restore procedure has succeeded.
