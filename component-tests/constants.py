METRICS_PORT = 51000
MAX_RETRIES = 5
BACKOFF_SECS = 7
MAX_LOG_LINES = 50

MANAGEMENT_HOST_NAME = "management.argotunnel.com"


def protocols():
    return ["http2", "quic"]
