#!/usr/bin/env python
import requests
from conftest import CfdModes
from constants import METRICS_PORT, MAX_RETRIES, BACKOFF_SECS
from retrying import retry
from cli import CloudflaredCli
from util import LOGGER, write_config, start_cloudflared, wait_tunnel_ready, send_requests
import platform

"""
Each test in TestManagement will:
1. Acquire a management token from Cloudflare public API
2. Make a request against the management service for the running tunnel
"""
class TestManagement:
    """
        test_get_host_details does the following:
        1. It gets a management token from Tunnelstore using cloudflared tail token <tunnel_id>
        2. It gets the connector_id after starting a cloudflare tunnel
        3. It sends a request to the management host with the connector_id and management token
        4. Asserts that the response has a hostname and ip.
    """
    def test_get_host_details(self, tmp_path, component_tests_config):
        # TUN-7377 : wait_tunnel_ready does not work properly in windows.
        # Skipping this test for windows for now and will address it as part of tun-7377
        if platform.system() == "Windows":
            return
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        headers = {}
        headers["Content-Type"] = "application/json"
        config_path = write_config(tmp_path, config.full_config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1", "--label" , "test"], cfd_args=["run", "--hello-world"], new_process=True):
            wait_tunnel_ready(tunnel_url=config.get_url(),
                              require_min_connections=1)
            cfd_cli = CloudflaredCli(config, config_path, LOGGER)
            connector_id = cfd_cli.get_connector_id(config)[0]
            url = cfd_cli.get_management_url("host_details", config, config_path)
            resp = send_request(url, headers=headers)
            
            # Assert response json.
            assert resp.status_code == 200, "Expected cloudflared to return 200 for host details"
            assert resp.json()["hostname"] == "custom:test", "Expected cloudflared to return hostname"
            assert resp.json()["ip"] != "", "Expected cloudflared to return ip"
            assert resp.json()["connector_id"] == connector_id, "Expected cloudflared to return connector_id"
    
    """
        test_get_metrics will verify that the /metrics endpoint returns the prometheus metrics dump
    """
    def test_get_metrics(self, tmp_path, component_tests_config):
        # TUN-7377 : wait_tunnel_ready does not work properly in windows.
        # Skipping this test for windows for now and will address it as part of tun-7377
        if platform.system() == "Windows":
            return
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        config_path = write_config(tmp_path, config.full_config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1", "--management-diagnostics"], new_process=True):
            wait_tunnel_ready(require_min_connections=1)
            cfd_cli = CloudflaredCli(config, config_path, LOGGER)
            url = cfd_cli.get_management_url("metrics", config, config_path)
            resp = send_request(url)
            
            # Assert response.
            assert resp.status_code == 200, "Expected cloudflared to return 200 for /metrics"
            assert "# HELP build_info Build and version information" in resp.text, "Expected /metrics to have with the build_info details"

    """
        test_get_pprof_heap will verify that the /debug/pprof/heap endpoint returns a pprof/heap dump response
    """
    def test_get_pprof_heap(self, tmp_path, component_tests_config):
        # TUN-7377 : wait_tunnel_ready does not work properly in windows.
        # Skipping this test for windows for now and will address it as part of tun-7377
        if platform.system() == "Windows":
            return
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        config_path = write_config(tmp_path, config.full_config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1", "--management-diagnostics"], new_process=True):
            wait_tunnel_ready(require_min_connections=1)
            cfd_cli = CloudflaredCli(config, config_path, LOGGER)
            url = cfd_cli.get_management_url("debug/pprof/heap", config, config_path)
            resp = send_request(url)
            
            # Assert response.
            assert resp.status_code == 200, "Expected cloudflared to return 200 for /debug/pprof/heap"
            assert resp.headers["Content-Type"] == "application/octet-stream", "Expected /debug/pprof/heap to have return a binary response"
    
    """
        test_get_metrics_when_disabled will verify that diagnostic endpoints (such as /metrics) return 404 and are unmounted. 
    """
    def test_get_metrics_when_disabled(self, tmp_path, component_tests_config):
        # TUN-7377 : wait_tunnel_ready does not work properly in windows.
        # Skipping this test for windows for now and will address it as part of tun-7377
        if platform.system() == "Windows":
            return
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        config_path = write_config(tmp_path, config.full_config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1"], new_process=True):
            wait_tunnel_ready(require_min_connections=1)
            cfd_cli = CloudflaredCli(config, config_path, LOGGER)
            url = cfd_cli.get_management_url("metrics", config, config_path)
            resp = send_request(url)
            
            # Assert response.
            assert resp.status_code == 404, "Expected cloudflared to return 404 for /metrics"


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def send_request(url, headers={}):
    with requests.Session() as s:
        return s.get(url, timeout=BACKOFF_SECS, headers=headers)
