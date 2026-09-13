locals {
  base_path      = "outputs"
  minio_username = "rootUser"
  minio_password = "rootPassword"
  minio_port     = 9000
  minio_bucket   = "ipxe"
}

resource "local_file" "minio-ca-cert" {
  filename = "${local.base_path}/minio/certs/CAs/ca.crt"
  content  = tls_self_signed_cert.minio-ca.cert_pem
}

resource "local_file" "minio-cert" {
  filename = "${local.base_path}/minio/certs/public.crt"
  content  = tls_locally_signed_cert.minio.cert_pem
}

resource "local_file" "minio-key" {
  filename = "${local.base_path}/minio/certs/private.key"
  content  = tls_private_key.minio.private_key_pem
}

resource "local_file" "minio-manifest" {
  filename = "${local.base_path}/minio.yaml"
  content = yamlencode({
    apiVersion = "v1"
    kind       = "Pod"
    metadata = {
      name      = "minio"
      namespace = "default"
    }
    spec = {
      hostNetwork = true
      containers = [
        {
          name  = "minio"
          image = "cgr.dev/chainguard/minio:latest"
          args = [
            "server",
            "--certs-dir",
            "/etc/minio/certs",
            "--address",
            "0.0.0.0:${local.minio_port}",
            "/var/lib/minio",
          ]
          env = [
            {
              name  = "MINIO_ROOT_USER"
              value = local.minio_username
            },
            {
              name  = "MINIO_ROOT_PASSWORD"
              value = local.minio_password
            },
          ]
          volumeMounts = [
            {
              name      = "data"
              mountPath = "/etc/minio/certs"
              subPath   = "minio/certs"
            },
            {
              name      = "tempdata"
              mountPath = "/var/lib/minio"
            },
          ]
        },
        {
          name  = "mc"
          image = "docker.io/minio/mc:latest"
          command = [
            "sh",
            "-c",
            <<-EOF
            set -e
            mc mb -p m/${local.minio_bucket}
            exec mc watch m
            EOF
          ]
          env = [
            {
              name  = "MC_HOST_m"
              value = "https://${local.minio_username}:${local.minio_password}@127.0.0.1:${local.minio_port}"
            },
          ]
          volumeMounts = [
            {
              name      = "data"
              mountPath = "/root/.mc/certs/CAs"
              subPath   = "minio/certs/CAs"
            },
          ]
        },
      ]
      volumes = [
        {
          name = "data"
          hostPath = {
            path = abspath(local.base_path)
          }
        },
        {
          name = "tempdata"
          emptyDir = {
            medium = "Memory"
          }
        },
      ]
    }
  })
  directory_permission = "0700"
  file_permission      = "0600"
}

resource "local_file" "server-ca-cert" {
  filename = "${local.base_path}/server/ca.crt"
  content  = tls_self_signed_cert.server-ca.cert_pem
}

resource "local_file" "server-cert" {
  filename = "${local.base_path}/server/tls.crt"
  content  = tls_locally_signed_cert.server.cert_pem
}

resource "local_file" "server-key" {
  filename = "${local.base_path}/server/tls.key"
  content  = tls_private_key.server.private_key_pem
}