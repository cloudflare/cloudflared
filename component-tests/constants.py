METRICS_PORT = 51000
MAX_RETRIES = 5
BACKOFF_SECS = 7

PROXY_DNS_PORT = 9053


def protocols():
    return ["h2mux", "http2", "quic"]
