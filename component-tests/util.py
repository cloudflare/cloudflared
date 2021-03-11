from contextlib import contextmanager
import logging
import requests
from retrying import retry
import subprocess
import yaml

from time import sleep

from constants import METRICS_PORT, MAX_RETRIES, BACKOFF_SECS

LOGGER = logging.getLogger(__name__)


def write_config(path, config):
    config_path = path / "config.yaml"
    with open(config_path, 'w') as outfile:
        yaml.dump(config, outfile)
    return config_path


def start_cloudflared(path, config, cfd_args=["run"], cfd_pre_args=["tunnel"], new_process=False, allow_input=False, capture_output=True):
    config_path = write_config(path, config.full_config)
    cmd = [config.cloudflared_binary]
    cmd += cfd_pre_args
    cmd += ["--config", config_path]
    cmd += cfd_args
    LOGGER.info(f"Run cmd {cmd} with config {config}")
    if new_process:
        return run_cloudflared_background(cmd, allow_input, capture_output)
    # By setting check=True, it will raise an exception if the process exits with non-zero exit code
    return subprocess.run(cmd, check=True, capture_output=capture_output)


@contextmanager
def run_cloudflared_background(cmd, allow_input, capture_output):
    output = subprocess.PIPE if capture_output else subprocess.DEVNULL
    stdin = subprocess.PIPE if allow_input else None
    try:
        cfd = subprocess.Popen(cmd, stdin=stdin, stdout=output, stderr=output)
        yield cfd
    finally:
        cfd.terminate()


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def wait_tunnel_ready(expect_connections=4):
    url = f'http://localhost:{METRICS_PORT}/ready'

    with requests.Session() as s:
        resp = send_request(s, url, True)
        assert resp.json()[
            "readyConnections"] == expect_connections, f"Ready endpoint returned {resp.json()} but we expect {expect_connections} ready connections"


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def check_tunnel_not_ready():
    url = f'http://localhost:{METRICS_PORT}/ready'

    resp = requests.get(url, timeout=1)
    assert resp.status_code == 503, f"Expect {url} returns 503, got {resp.status_code}"

# In some cases we don't need to check response status, such as when sending batch requests to generate logs


def send_requests(url, count, require_ok=True):
    errors = 0
    with requests.Session() as s:
        for _ in range(count):
            resp = send_request(s, url, require_ok)
            if resp is None:
                errors += 1
            sleep(0.01)
    if errors > 0:
        LOGGER.warning(
            f"{errors} out of {count} requests to {url} return non-200 status")


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def send_request(session, url, require_ok):
    resp = session.get(url, timeout=BACKOFF_SECS)
    if require_ok:
        assert resp.status_code == 200, f"{url} returned {resp}"
    return resp if resp.status_code == 200 else None
