# ipxe-presign

A small HTTPS iPXE boot-script server for bare-metal node provisioning, in the
spirit of [Matchbox](https://github.com/poseidon/matchbox) but with everything
that isn't needed here cut out.

Nodes PXE-boot into iPXE, which `chain.load`s this server over **mTLS**. The
client certificate's **commonName selects the boot profile**; the rendered
script contains the profile's kernel/initrd URLs plus **short-lived
pre-signed S3 (MinIO) URLs** for the sensitive ignition and rootfs objects —
so nodes need no long-term credentials to fetch boot-critical data.

## Example rendered script

```
#!ipxe
kernel https://minio.internal:9000/boot-assets/fcos/vmlinuz console=tty0 console=ttyS0,115200n8 ignition.firstboot ignition.platform.id=metal ignition.url=https://minio.internal:9000/boot-assets/ignition/worker.ign?X-Amz-...&X-Amz-Signature=... rootfs.url=https://minio.internal:9000/boot-assets/fcos/worker-rootfs.img?X-Amz-...&X-Amz-Signature=...
initrd https://minio.internal:9000/boot-assets/fcos/initramfs.img
boot
```

A client whose CN matches no profile gets:

```
#!ipxe
exit
```

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
| `-s3-endpoint` | — | S3/MinIO endpoint, `host:port` or with `http://`/`https://` prefix (required) |
| `-s3-region` | `us-east-1` | Region used for SigV4 signing. Must be set so presigning never queries the endpoint for the bucket location (MinIO's default region is `us-east-1`) |
| `-s3-bucket` | — | Bucket holding the ignition and rootfs objects (required) |
| `-presign-duration` | `60s` | Validity of pre-signed URLs |
| `-config` | — | Path to the YAML profile config (required) |
| `-tls-cert` / `-tls-key` | — | Server certificate/key (required) |
| `-tls-ca` | — | CA used to verify client certificates (required) |

Credentials come from `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`
(any S3-compatible static credentials work; scope the key to read-only on the
bucket).

TLS is `RequireAndVerifyClientCert` — connections without a certificate
signed by the configured CA fail the handshake.

## Profile config (`-config`)

```yaml
profiles:
  worker-profile:
    kernel_url: "https://minio.internal:9000/boot-assets/fcos/vmlinuz"
    initrd_urls:
      - "https://minio.internal:9000/boot-assets/fcos/initramfs.img"
    ignition_s3_resource: "ignition/worker.ign"
    rootfs_s3_resource: "fcos/worker-rootfs.img"
    kargs:
      - "console=tty0"
      - "console=ttyS0,115200n8"
      - "ignition.firstboot"
      - "ignition.platform.id=metal"
```

- Keys under `profiles` are matched **exactly** against the client cert CN.
- `ignition_s3_resource` / `rootfs_s3_resource` are object keys inside
  `-s3-bucket`, not URLs — the server presigns them.
- The presigned URLs are appended to the kernel line as
  `ignition.url=<presigned>` and `rootfs.url=<presigned>`. If your
  initramfs expects different argument names, edit the template in
  `render.go`.

## Deployment notes

- **Cilium Gateway API**: this needs *passthrough* TLS termination (mTLS to
  the pod), not Gateway API TLS termination with a leaf cert — the client
  handshake happens directly with this server.
- **iPXE client side**: iPXE must be told the CA (`set -s ipxe_ca ...` +
  `ssl-verify-peer`, or a pre-seeded CA) and presented its client
  cert. CNs like `worker-profile` / `control-plane` map straight to profiles.
- Presigning is local (SigV4 over static credentials) and performs no
  network round-trip, so node boot does not depend on this server reaching
  MinIO — only the node's final GET of the presigned URLs does.
- Keep `-presign-duration` short; iPXE fetches happen within seconds of
  script render, so 60s is plenty.

## Layout

| File | Purpose |
|------|---------|
| `main.go` | flags, TLS config (mTLS), server, graceful shutdown |
| `config.go` | YAML profile config load + validation |
| `presign.go` | minio-go client + presigned URL generation |
| `handler.go` | CN → profile lookup, dispatch, `/healthz` |
| `render.go` | iPXE script template + exit script |
| `profiles.example.yaml` | example config |
