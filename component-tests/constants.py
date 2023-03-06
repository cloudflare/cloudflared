METRICS_PORT = 51000
MAX_RETRIES = 5
BACKOFF_SECS = 7
MAX_LOG_LINES = 50

PROXY_DNS_PORT = 9053


def protocols():
    return ["http2", "quic"]
