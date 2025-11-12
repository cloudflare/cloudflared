#!/usr/bin/env python
import requests
from conftest import CfdModes
from constants import METRICS_PORT, MAX_RETRIES, BACKOFF_SECS
from retrying import retry
from cli import CloudflaredCli
from util import LOGGER, write_config, start_cloudflared, wait_tunnel_ready, send_requests
import platform

class TestTunnel:
    '''Test tunnels with no ingress rules from config.yaml but ingress rules from CLI only'''

    def test_tunnel_hello_world(self, tmp_path, component_tests_config):
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1"],  cfd_args=["run", "--hello-world"], new_process=True):
            wait_tunnel_ready(tunnel_url=config.get_url(),
                              require_min_connections=1)
    
    def test_tunnel_url(self, tmp_path, component_tests_config):
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1"],  cfd_args=["run", "--url", f"http://localhost:{METRICS_PORT}/"], new_process=True):
            wait_tunnel_ready(require_min_connections=1)
            send_requests(config.get_url()+"/ready", 3, True)

    def test_tunnel_no_ingress(self, tmp_path, component_tests_config):
        '''
        Running a tunnel with no ingress rules provided from either config.yaml or CLI will still work but return 503
        for all incoming requests.
        '''
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1"],  cfd_args=["run"], new_process=True):
            wait_tunnel_ready(require_min_connections=1)
            expected_status_code = 503
            resp = send_request(config.get_url()+"/", expected_status_code)
            assert resp.status_code == expected_status_code, "Expected cloudflared to return 503 for all requests with no ingress defined"
            resp = send_request(config.get_url()+"/test", expected_status_code)
            assert resp.status_code == expected_status_code, "Expected cloudflared to return 503 for all requests with no ingress defined"

def retry_if_result_none(result):
    '''
    Returns True if the result is None, indicating that the function should be retried.
    '''
    return result is None

@retry(retry_on_result=retry_if_result_none, stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def send_request(url, expected_status_code=200):
    with requests.Session() as s:
        resp = s.get(url, timeout=BACKOFF_SECS)
        return resp if resp.status_code == expected_status_code else None
