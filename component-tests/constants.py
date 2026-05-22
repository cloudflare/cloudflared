METRICS_PORT = 51000
MAX_RETRIES = 5
BACKOFF_SECS = 7
MAX_LOG_LINES = 50

MANAGEMENT_HOST_NAME = "management.argotunnel.com"

# How long to wait for the cloudflared process to exit after SIGTERM before
# sending SIGKILL.
GRACEFUL_SHUTDOWN_TIMEOUT = 10
# How long to wait for each pipe reader thread to finish after the process
# exits.
READER_THREAD_JOIN_TIMEOUT = 5
# How long to wait for an expected log message to appear before giving up.
LOG_POLL_TIMEOUT = 30
# How often to re-check the accumulated log lines while polling.
LOG_POLL_INTERVAL = 0.5


def protocols():
    return ["http2", "quic"]
