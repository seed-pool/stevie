#!/bin/sh
set -eu

# Trim + lowercase domain. Empty / localhost / 127.0.0.1 → local HTTP mode.
DOMAIN="$(printf '%s' "${STEVIE_DOMAIN:-}" | tr '[:upper:]' '[:lower:]' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
MODE="${TLS_MODE:-internal}"
LISTEN="${STEVIE_LISTEN_PORT:-${STEVIE_HTTP_PORT:-8080}}"
HTTPS_PORT="${STEVIE_HTTPS_PORT:-8443}"

LOCAL=0
case "$DOMAIN" in
""|localhost|127.0.0.1) LOCAL=1 ;;
esac

# No real domain → serve cleartext on STEVIE_LISTEN_PORT (http://localhost:PORT).
if [ "$LOCAL" = "1" ]; then
  MODE=off
fi

if [ "$MODE" = "off" ]; then
  # Localhost, or behind an external HTTPS reverse proxy.
  cat > /tmp/Caddyfile <<CADDY
{
	auto_https off
	http_port ${LISTEN}
}

:${LISTEN} {
	@api path /api/* /healthz /readyz
	handle @api {
		reverse_proxy server:8080
	}
	handle {
		encode gzip
		reverse_proxy web:80
	}
}
CADDY
elif [ "$MODE" = "files" ]; then
  if [ ! -f /certs/tls.crt ] || [ ! -f /certs/tls.key ]; then
    echo "TLS_MODE=files requires /certs/tls.crt and /certs/tls.key" >&2
    exit 1
  fi
  TLS_LINE="tls /certs/tls.crt /certs/tls.key"
else
  TLS_LINE="tls internal"
fi

if [ "$MODE" != "off" ]; then
  if [ -z "$DOMAIN" ]; then
    echo "STEVIE_DOMAIN is required when TLS_MODE=${MODE}" >&2
    exit 1
  fi
  cat > /tmp/Caddyfile <<EOF_INNER
{
	auto_https disable_redirects
	https_port ${HTTPS_PORT}
}

${DOMAIN}:${HTTPS_PORT} {
	${TLS_LINE}

	@api path /api/* /healthz /readyz
	handle @api {
		reverse_proxy server:8080
	}

	handle {
		encode gzip
		reverse_proxy web:80
	}
}
EOF_INNER
fi

if [ "$LOCAL" = "1" ]; then
  echo "Stevie local mode: http://127.0.0.1:${LISTEN}"
fi

exec caddy run --config /tmp/Caddyfile --adapter caddyfile
