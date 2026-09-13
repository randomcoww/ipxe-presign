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
   `/ipxe` with the node's MAC address.

2. The **`/ipxe`** endpoint looks the MAC up in the `groups:` config section,
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
| `-server-cert` / `-server-key` | — | Server certificate/key (required) |
| `-trusted-ca` | — | CA used to verify client certificates (required) |

Credentials come from `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`
(any S3-compatible static credentials work; scope the key to read-only on the
bucket).

## Profile config (`-config`)

```yaml
global:
  kernel_s3_resource: "fcos/vmlinuz"
  initrd_s3_resources:
  - "fcos/initramfs.img"
  ignition_s3_resource: "ignition/worker.ign"
  rootfs_s3_resource: "fcos/worker-rootfs.img"
  kargs:
  - "ignition.firstboot"
  - "ignition.platform.id=metal"

overlays:
- selector:
  - "aa-bb-cc-dd-ee-01"
  - "aa-bb-cc-dd-ee-02"
  ignition_s3_resource: "ignition/worker-v1.ign"
  kargs:
  - "console=tty0"

- selector:
  - "aa:bb:cc:dd:ee:02"
  kargs:
  - "console=ttyS0,115200n8"
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
| `GET /boot.ipxe` | First-stage entry script (static template + advertise URL) |
| `GET /ipxe?mac=...` | Second-stage boot script, profile selected by MAC |
| `GET /healthz` | Liveness probe (still behind mTLS) |

## MinIO instance for testing

```bash
tofu() {
  set -x
  podman run -it --rm --security-opt label=disable \
    -v $(pwd):$(pwd) \
    -w $(pwd) \
    --net=host \
    ghcr.io/opentofu/opentofu:latest "$@"
  rc=$?; set +x; return $rc
}
```

```bash
tofu -chdir=test init -upgrade && tofu -chdir=test apply
```

```bash
podman play kube test/outputs/minio.yaml
```

```bash
AWS_ACCESS_KEY_ID=minioUser \
AWS_SECRET_ACCESS_KEY=minioPassword \
go run main.go \
  -s3-endpoint https://127.0.0.1:9000 \
  -s3-bucket ipxe \
  -config test/config.yaml.sample \
  -server-cert test/outputs/server/tls.crt \
  -server-key test/outputs/server/tls.key \
  -trusted-ca test/outputs/server/ca.crt \
  -listen-address 0.0.0.0:8080 \
  -advertise-url https://ipxe.local:8080 \
  -client-cns ipxe-node
```

```bash
podman run -it --rm  \
  -e AWS_ACCESS_KEY_ID=minioUser \
  -e AWS_SECRET_ACCESS_KEY=minioPassword \
  -v $(pwd)/test:/config \
  -p 8080:8080 \
  test \
  -s3-endpoint https://127.0.0.1:9000 \
  -s3-bucket ipxe \
  -config /config/config.yaml.sample \
  -server-cert /config/outputs/server/tls.crt \
  -server-key /config/outputs/server/tls.key \
  -trusted-ca /config/outputs/server/ca.crt \
  -listen-address 0.0.0.0:8080 \
  -advertise-url https://ipxe.local:8080 \
  -client-cns ipxe-node
```