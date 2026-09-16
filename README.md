# ipxe-presign

A small mTLS iPXE boot-script server for bare-metal node provisioning, in the
spirit of [Matchbox](https://github.com/poseidon/matchbox) but with everything
that isn't needed here cut out.

## How it works

1. A node PXE-boots into iPXE, which fetches the **entry script** from this
   server over mTLS:

   ```
   #!ipxe
   chain https://ipxe.internal:8443/pxe?mac:hexhyp=${mac:hexhyp}&uuid=${uuid}
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
| `-config` | — | Path to the YAML profile config (required) |

Credentials come from `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`
(any S3-compatible static credentials work; scope the key to read-only on the
bucket).

## Profile config (`-config`)

```yaml
listen: 0.0.0.0:8443
serverCert: /config/outputs/server/tls.crt
serverKey: /config/outputs/server/tls.key
trustedCAs:
- /config/outputs/server/ca.crt
allowedClientCNs:
- ipxe-node-1
s3Endpoint: https://127.0.0.1:9000
s3Bucket: boot
s3TrustedCAs:
- /config/outputs/minio/certs/CAs/ca.crt
PresignTTL: 60s
advertiseURL: https://ipxe.local:8443

profiles:
- selector:
    "mac:hexhyp":
    - aa-bb-cc-dd-ee-01
    - aa-bb-cc-dd-ee-02
    "buildarch:uristring":
    - x86_64
  kernelURL: '{{ presign "fcos/vmlinuz-${buildarch:uristring}" }}'
  initrdURLs:
  - '{{ presign "fcos/initramfs-${buildarch:uristring}.img" }}'
  kargs:
  - console=tty0
  - 'ignition.config.url={{ presign "ignition/worker-${mac:hexhyp}.ign" }}'
  - 'coreos.live.rootfs_url={{ presign "fcos/rootfs-${buildarch:uristring}.img" }}'
- selector:
    "mac:hexhyp":
    - aa-bb-cc-dd-ee-02
  kargs:
  - ignition.firstboot
```

- `profiles` keys are profile names. Every value is an **object key inside 
  `-s3-bucket`**, not a URL — the server presigns it.
- MACs are matched case-insensitively. The canonical `aa-bb-cc-dd-ee-ff`
  form is expected (it's what iPXE sends as `${mac:hexhyp}`).
- The presigned ignition and rootfs URLs are appended to the kernel line as
  `ignition.config.url=<presigned>` and `coreos.live.rootfs_url=<presigned>`.

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
podman run -it --rm  \
  -e AWS_ACCESS_KEY_ID=minioUser \
  -e AWS_SECRET_ACCESS_KEY=minioPassword \
  -v $(pwd)/test:/config \
  -p 8080:8080 \
  -p 8443:8443 \
  test \
  -config /config/config.yaml.sample
```