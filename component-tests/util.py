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


def start_cloudflared(path, component_test_config, cfd_args=["run"], cfd_pre_args=["tunnel"], new_process=False, classic=False, capture_output=True):
    if classic:
        config = component_test_config.classic_tunnel_config.full_config
    else:
        config = component_test_config.named_tunnel_config.full_config
    config_path = write_config(path, config)
    cmd = [component_test_config.cloudflared_binary]
    cmd += cfd_pre_args
    cmd += ["--config", config_path]
    cmd += cfd_args
    LOGGER.info(f"Run cmd {cmd} with config {config}")
    if new_process:
        return run_cloudflared_background(cmd, capture_output)
    # By setting check=True, it will raise an exception if the process exits with non-zero exit code
    return subprocess.run(cmd, check=True, capture_output=capture_output)


@contextmanager
def run_cloudflared_background(cmd, capture_output):
    output = subprocess.PIPE if capture_output else subprocess.DEVNULL
    try:
        cfd = subprocess.Popen(cmd, stdout=output, stderr=output)
        yield cfd
    finally:
        cfd.terminate()


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def wait_tunnel_ready():
    url = f'http://localhost:{METRICS_PORT}/ready'
    send_requests(url, 1)

# In some cases we don't need to check response status, such as when sending batch requests to generate logs


def send_requests(url, count, require_ok=True):
    errors = 0
    with requests.Session() as s:
        for _ in range(count):
            ok = send_request(s, url, require_ok)
            if not ok:
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
    return True if resp.status_code == 200 else False
