import logging
import os
import platform
import subprocess
from contextlib import contextmanager
from time import sleep

import pytest

import requests
import yaml
from retrying import retry

from constants import METRICS_PORT, MAX_RETRIES, BACKOFF_SECS

LOGGER = logging.getLogger(__name__)

def select_platform(plat):
    return pytest.mark.skipif(
        platform.system() != plat, reason=f"Only runs on {plat}")


def write_config(directory, config):
    config_path = directory / "config.yml"
    with open(config_path, 'w') as outfile:
        yaml.dump(config, outfile)
    return config_path


def start_cloudflared(directory, config, cfd_args=["run"], cfd_pre_args=["tunnel"], new_process=False,
                      allow_input=False, capture_output=True, root=False, skip_config_flag=False):

    config_path = None
    if not skip_config_flag:
        config_path = write_config(directory, config.full_config)

    cmd = cloudflared_cmd(config, config_path, cfd_args, cfd_pre_args, root)

    if new_process:
        return run_cloudflared_background(cmd, allow_input, capture_output)
    # By setting check=True, it will raise an exception if the process exits with non-zero exit code
    return subprocess.run(cmd, check=True, capture_output=capture_output)


def cloudflared_cmd(config, config_path, cfd_args, cfd_pre_args, root):
    cmd = []
    if root:
        cmd += ["sudo"]
    cmd += [config.cloudflared_binary]
    cmd += cfd_pre_args

    if config_path is not None:
        cmd += ["--config", str(config_path)]

    cmd += cfd_args
    LOGGER.info(f"Run cmd {cmd} with config {config}")
    return cmd


@contextmanager
def run_cloudflared_background(cmd, allow_input, capture_output):
    output = subprocess.PIPE if capture_output else subprocess.DEVNULL
    stdin = subprocess.PIPE if allow_input else None
    try:
        cfd = subprocess.Popen(cmd, stdin=stdin, stdout=output, stderr=output)
        yield cfd
    finally:
        cfd.terminate()
        if capture_output:
            LOGGER.info(f"cloudflared log: {cfd.stderr.read()}")


def wait_tunnel_ready(tunnel_url=None, require_min_connections=1, cfd_logs=None):
    try:
        inner_wait_tunnel_ready(tunnel_url, require_min_connections)
    except Exception as e:
        if cfd_logs is not None:
            _log_cloudflared_logs(cfd_logs)
        raise e


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def inner_wait_tunnel_ready(tunnel_url=None, require_min_connections=1):
    metrics_url = f'http://localhost:{METRICS_PORT}/ready'

    with requests.Session() as s:
        resp = send_request(s, metrics_url, True)

        assert resp.json()["readyConnections"] >= require_min_connections, \
            f"Ready endpoint returned {resp.json()} but we expect at least {require_min_connections} connections"

        if tunnel_url is not None:
            send_request(s, tunnel_url, True)


def _log_cloudflared_logs(cfd_logs):
    log_file = cfd_logs
    if os.path.isdir(cfd_logs):
        files = os.listdir(cfd_logs)
        if len(files) == 0:
            return
        log_file = os.path.join(cfd_logs, files[0])
    with open(log_file, "r") as f:
        LOGGER.warning("Cloudflared Tunnel was not ready:")
        for line in f.readlines():
            LOGGER.warning(line)


@retry(stop_max_attempt_number=MAX_RETRIES * BACKOFF_SECS, wait_fixed=1000)
def check_tunnel_not_connected():
    url = f'http://localhost:{METRICS_PORT}/ready'

    try:
        resp = requests.get(url, timeout=1)
        assert resp.status_code == 503, f"Expect {url} returns 503, got {resp.status_code}"
    # cloudflared might already terminate
    except requests.exceptions.ConnectionError as e:
        LOGGER.warning(f"Failed to connect to {url}, error: {e}")


def get_tunnel_connector_id():
    url = f'http://localhost:{METRICS_PORT}/ready'

    try:
        resp = requests.get(url, timeout=1)
        return resp.json()["connectorId"]
    # cloudflared might already terminated
    except requests.exceptions.ConnectionError as e:
        LOGGER.warning(f"Failed to connect to {url}, error: {e}")


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
