# ipxe-presign

A small mTLS iPXE boot-script server for bare-metal node provisioning, in the
spirit of [Matchbox](https://github.com/poseidon/matchbox) but with everything
that isn't needed here cut out.

## How it works

1. A node PXE-boots into iPXE, which fetches the **entry script** from this
   server over mTLS:

   ```
   #!ipxe
   chain https://ipxe.internal:8443/pxe?mac=${mac:hexhyp}&uuid=${uuid}
   ```

   iPXE substitutes `${mac:hexhyp}` / `${uuid}` at parse time and chains to
   `/pxe` with the node's MAC address.

2. The **`/pxe`** endpoint looks the MAC up in the `groups:` config section,
   resolves it to a boot profile, and renders the boot script with
   **short-lived pre-signed S3 (MinIO) URLs** for kernel, initrd, ignition
   and rootfs:

   ```
   #!ipxe
   kernel https://minio.internal:9000/boot-assets/fcos/vmlinuz?X-Amz-... console=tty0 ... ignition.url=https://minio.internal:9000/boot-assets/ignition/worker.ign?X-Amz-... rootfs.url=https://minio.internal:9000/boot-assets/fcos/worker-rootfs.img?X-Amz-...
   initrd https://minio.internal:9000/boot-assets/fcos/initramfs.img?X-Amz-...
   boot
   ```

   Nodes never need long-term credentials — only the (expiring) signed URLs.

3. A MAC that matches no group gets:

   ```
   #!ipxe
   exit
   ```

**TLS is the authentication layer, not the identity layer.** The client
certificate must be signed by the configured CA (enforced at the TLS layer —
no cert, no connection) and its commonName must be in the `-client-cns`
allowlist (enforced by the server, 403 otherwise). The *profile* is selected
purely by MAC address.

## Build

```
make            # or: go build -o ipxe-presign .
make test       # go test ./...
```

## Run

```
AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... \
ipxe-presign \
  -listen-address 0.0.0.0:8443 \
  -advertise-url https://ipxe.internal:8443 \
  -client-cns ipxe-node-1,ipxe-node-2 \
  -s3-endpoint minio.internal:9000 \
  -s3-bucket boot-assets \
  -s3-region us-east-1 \
  -presign-duration 60s \
  -config profiles.yaml \
  -tls-cert /etc/ipxe-presign/server.crt \
  -tls-key  /etc/ipxe-presign/server.key \
  -tls-ca   /etc/ipxe-presign/ca.crt
```

| Flag | Default | Description |
|------|---------|-------------|
| `-listen-address` | `0.0.0.0:8443` | Listen address (always TLS) |
| `-advertise-url` | — | Public base URL iPXE uses to reach this server; used to build the `chain` URL in the entry script (required) |
| `-client-cns` | — | Comma-separated allowlist of client certificate commonNames (required) |
| `-s3-endpoint` | — | S3/MinIO endpoint, `host:port` or with `http://`/`https://` prefix (required) |
| `-s3-region` | `us-east-1` | Region used for SigV4 signing. Set it so presigning never queries the endpoint for the bucket location (MinIO's default region is `us-east-1`) |
| `-s3-bucket` | — | Bucket holding all boot resources (required) |
| `-presign-duration` | `60s` | Validity of pre-signed URLs |
| `-config` | — | Path to the YAML profile config (required) |
| `-tls-cert` / `-tls-key` | — | Server certificate/key (required) |
| `-tls-ca` | — | CA used to verify client certificates (required) |

Credentials come from `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`
(any S3-compatible static credentials work; scope the key to read-only on the
bucket).

## Profile config (`-config`)

```yaml
profiles:
  worker-profile:
    kernel_s3_resource: "fcos/vmlinuz"
    initrd_s3_resources:
      - "fcos/initramfs.img"
    ignition_s3_resource: "ignition/worker.ign"
    rootfs_s3_resource: "fcos/worker-rootfs.img"
    kargs:
      - "console=tty0"
      - "console=ttyS0,115200n8"
      - "ignition.firstboot"
      - "ignition.platform.id=metal"

groups:
  worker-profile:
    - "aa:bb:cc:dd:ee:01"
    - "aa:bb:cc:dd:ee:02"
  control-plane:
    - "aa:bb:cc:dd:ee:f1"
```

- `profiles` keys are profile names. Every `*_s3_resource` /
  `*_s3_resources` value is an **object key inside `-s3-bucket`**, not a URL —
  the server presigns it.
- `groups` maps MAC addresses to profile names. Group names must be existing
  profile names; each MAC may appear in exactly one group (enforced at
  startup, as are missing profile fields).
- MACs are matched case-insensitively. The canonical `aa:bb:cc:dd:ee:ff`
  form is expected (it's what iPXE sends as `${mac:hexhyp}`), but dashed,
  dotted and bare 12-hex-digit forms are accepted in config.
- The presigned ignition and rootfs URLs are appended to the kernel line as
  `ignition.url=<presigned>` and `rootfs.url=<presigned>`. If your initramfs
  expects different argument names, edit the template in `render.go`.

## Endpoints

| Path | Purpose |
|------|---------|
| `GET /` | First-stage entry script (static template + advertise URL) |
| `GET /pxe?mac=...` | Second-stage boot script, profile selected by MAC |
| `GET /healthz` | Liveness probe (still behind mTLS) |

## Deployment notes

- **Gateway/ingress**: TLS must terminate at this pod (mTLS passthrough) —
  the client handshake happens directly with this server, and the server
  needs the verified client cert for the CN allowlist.
- **iPXE client side**: iPXE must trust the server CA (`ssl-verify-peer` /
  pre-seeded CA) and be issued a client cert whose CN is in `-client-cns`.
  CNs here are *identities of iPXE instances*, e.g. `ipxe-node-1` — not
  profile names.
- Presigning is local (SigV4 over static credentials, no network round-trip
  when the region is set), so this server does not need to reach MinIO to
  serve scripts — only the node's final GET of the presigned URLs does.
- Keep `-presign-duration` short; the node's GETs happen within seconds of
  script render, so 60s is plenty.

## Layout

| File | Purpose |
|------|---------|
| `main.go` | flags, TLS config (mTLS), server, graceful shutdown |
| `config.go` | YAML profile/group config, MAC normalization, validation |
| `presign.go` | minio-go client + presigned URL generation |
| `handler.go` | CN allowlist, `/`, `/pxe` MAC routing, `/healthz` |
| `render.go` | entry script + boot script templates, exit script |
| `profiles.example.yaml` | example config |
