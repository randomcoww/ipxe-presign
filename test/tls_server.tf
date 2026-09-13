resource "tls_private_key" "server" {
  algorithm   = tls_private_key.server-ca.algorithm
  ecdsa_curve = "P521"
}

resource "tls_cert_request" "server" {
  private_key_pem = tls_private_key.server.private_key_pem

  subject {
    common_name = "server"
  }

  ip_addresses = distinct([
    "127.0.0.1",
  ])
}

resource "tls_locally_signed_cert" "server" {
  cert_request_pem   = tls_cert_request.server.cert_request_pem
  ca_private_key_pem = tls_private_key.server-ca.private_key_pem
  ca_cert_pem        = tls_self_signed_cert.server-ca.cert_pem

  validity_period_hours = 8760

  allowed_uses = [
    "key_encipherment",
    "digital_signature",
    "server_auth",
  ]
}