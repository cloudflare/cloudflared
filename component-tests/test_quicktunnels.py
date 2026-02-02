#!/usr/bin/env python
from conftest import CfdModes
from constants import METRICS_PORT
import time
from util import LOGGER, start_cloudflared, wait_tunnel_ready, get_quicktunnel_url, send_requests

class TestQuickTunnels:
    def test_quick_tunnel(self, tmp_path, component_tests_config):
        config = component_tests_config(cfd_mode=CfdModes.QUICK)
        LOGGER.debug(config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1"], cfd_args=["--hello-world"], new_process=True):
            wait_tunnel_ready(require_min_connections=1)
            time.sleep(10)
            url = get_quicktunnel_url()
            send_requests(url, 3, True)
    
    def test_quick_tunnel_url(self, tmp_path, component_tests_config):
        config = component_tests_config(cfd_mode=CfdModes.QUICK)
        LOGGER.debug(config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1"], cfd_args=["--url", f"http://localhost:{METRICS_PORT}/"], new_process=True):
            wait_tunnel_ready(require_min_connections=1)
            time.sleep(10)
            url = get_quicktunnel_url()
            send_requests(url+"/ready", 3, True)
