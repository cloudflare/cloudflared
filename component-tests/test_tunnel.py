#!/usr/bin/env python
import requests
from conftest import CfdModes
from constants import METRICS_PORT, MAX_RETRIES, BACKOFF_SECS
from retrying import retry
from util import LOGGER, start_cloudflared, wait_tunnel_ready, send_requests

class TestTunnel:
    '''Test tunnels with no ingress rules from config.yaml but ingress rules from CLI only'''

    def test_tunnel_hello_world(self, tmp_path, component_tests_config):
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        with start_cloudflared(tmp_path, config, cfd_args=["run", "--hello-world"], new_process=True):
            wait_tunnel_ready(tunnel_url=config.get_url(),
                              require_min_connections=4)
    
    def test_tunnel_url(self, tmp_path, component_tests_config):
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        with start_cloudflared(tmp_path, config, cfd_args=["run", "--url", f"http://localhost:{METRICS_PORT}/"], new_process=True):
            wait_tunnel_ready(require_min_connections=4)
            send_requests(config.get_url()+"/ready", 3, True)

    def test_tunnel_no_ingress(self, tmp_path, component_tests_config):
        '''
        Running a tunnel with no ingress rules provided from either config.yaml or CLI will still work but return 502
        for all incoming requests.
        '''
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        with start_cloudflared(tmp_path, config, cfd_args=["run"], new_process=True):
            wait_tunnel_ready(require_min_connections=4)
            resp = send_request(config.get_url()+"/")
            assert resp.status_code == 502, "Expected cloudflared to return 502 for all requests with no ingress defined"
            resp = send_request(config.get_url()+"/test")
            assert resp.status_code == 502, "Expected cloudflared to return 502 for all requests with no ingress defined"


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def send_request(url):
    with requests.Session() as s:
        return s.get(url, timeout=BACKOFF_SECS)
