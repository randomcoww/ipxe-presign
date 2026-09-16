# ipxe-presign

A small mTLS iPXE boot-script server for bare-metal node provisioning, in the
spirit of [Matchbox](https://github.com/poseidon/matchbox) but with everything
that isn't needed here cut out.

## How it works

1. A node PXE-boots into iPXE, which fetches the **entry script** from this
   server over mTLS:

   ```
   #!ipxe
   chain ipxe?mac:hexhyp=${mac:hexhyp}&buildarch:uristring=${buildarch:uristring}&uuid=${uuid}
   ```

   iPXE substitutes `${mac:hexhyp}` / `${uuid}` at parse time and chains to
   `/ipxe` with the node's MAC address.

2. The **`/ipxe`** endpoint matches boot profiles by iPXE parameters as selectors
   (e.g. mac:hexhyp: aa-bb-cc-dd-ee-01). An iPXE boot script is rendered including 
   pre-signed S3 (MinIO) URLs where specified:

   ```
   #!ipxe
   kernel https://minio.internal:9000/boot-assets/fcos/vmlinuz?X-Amz-... console=tty0 ... ignition.config.url=https://minio.internal:9000/boot-assets/ignition/worker.ign?X-Amz-... coreos.live.rootfs_url=https://minio.internal:9000/boot-assets/fcos/worker-rootfs.img?X-Amz-...
   initrd https://minio.internal:9000/boot-assets/fcos/initramfs.img?X-Amz-...
   boot
   ```

   Nodes never need long-term credentials — only the (expiring) signed URLs.

3. No matches by selector:

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
make      # or: go build -o ipxe-presign .
make test # go test ./...
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

- iPXE variables such as `${buildarch:uristring}` are rendered with params
  returned from iPXE.
- A S3 (MinIO) object may be pre-signed by including an entry like
  `{{ presign "fcos/initramfs-${buildarch:uristring}.img" }}`. The object
  name is first resolved by param such as `fcos/initramfs-aa-bb-cc-dd-ee-01.img`,
  and converted to a pre-signed URL of a S3 object at
  `<s3Endpoint>/<s3Bucket>/fcos/initramfs-aa-bb-cc-dd-ee-01.img`.

## Endpoints

| Path | Purpose |
|------|---------|
| `GET /boot.ipxe` | First-stage entry script (static template + advertise URL) |
| `GET /ipxe?mac=...` | Second-stage boot script, profile selected by MAC |

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