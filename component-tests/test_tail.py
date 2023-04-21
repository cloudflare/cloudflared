#!/usr/bin/env python
import asyncio
import json
import pytest
import requests
from websockets.client import connect, WebSocketClientProtocol
from conftest import CfdModes
from constants import MAX_RETRIES, BACKOFF_SECS
from retrying import retry
from cli import CloudflaredCli
from util import LOGGER, start_cloudflared, write_config, wait_tunnel_ready

class TestTail:
    @pytest.mark.asyncio
    async def test_start_stop_streaming(self, tmp_path, component_tests_config):
        """
        Validates that a websocket connection to management.argotunnel.com/logs can be opened
        with the access token and start and stop streaming on-demand.
        """
        print("test_start_stop_streaming")
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        headers = {}
        headers["Content-Type"] = "application/json"
        config_path = write_config(tmp_path, config.full_config)
        with start_cloudflared(tmp_path, config, cfd_args=["run", "--hello-world"], new_process=True):
            wait_tunnel_ready(tunnel_url=config.get_url(),
                              require_min_connections=1)
            cfd_cli = CloudflaredCli(config, config_path, LOGGER)
            url = cfd_cli.get_management_wsurl("logs", config, config_path)
            async with connect(url, open_timeout=5, close_timeout=3) as websocket:
                await websocket.send('{"type": "start_streaming"}')
                await websocket.send('{"type": "stop_streaming"}')
                await websocket.send('{"type": "start_streaming"}')
                await websocket.send('{"type": "stop_streaming"}')

    @pytest.mark.asyncio
    async def test_streaming_logs(self, tmp_path, component_tests_config):
        """
        Validates that a streaming logs connection will stream logs
        """
        print("test_streaming_logs")
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        headers = {}
        headers["Content-Type"] = "application/json"
        config_path = write_config(tmp_path, config.full_config)
        with start_cloudflared(tmp_path, config, cfd_args=["run", "--hello-world"], new_process=True):
            wait_tunnel_ready(tunnel_url=config.get_url(),
                              require_min_connections=1)
            cfd_cli = CloudflaredCli(config, config_path, LOGGER)
            url = cfd_cli.get_management_wsurl("logs", config, config_path)
            async with connect(url, open_timeout=5, close_timeout=5) as websocket:
                # send start_streaming
                await websocket.send('{"type": "start_streaming"}')
                # send some http requests to the tunnel to trigger some logs
                await asyncio.wait_for(generate_and_validate_log_event(websocket, config.get_url()), 10)
                # send stop_streaming
                await websocket.send('{"type": "stop_streaming"}')

    @pytest.mark.asyncio
    async def test_streaming_logs_filters(self, tmp_path, component_tests_config):
        """
        Validates that a streaming logs connection will stream logs 
        but not http when filters applied.
        """
        print("test_streaming_logs_filters")
        config = component_tests_config(cfd_mode=CfdModes.NAMED, run_proxy_dns=False, provide_ingress=False)
        LOGGER.debug(config)
        headers = {}
        headers["Content-Type"] = "application/json"
        config_path = write_config(tmp_path, config.full_config)
        with start_cloudflared(tmp_path, config, cfd_args=["run", "--hello-world"], new_process=True):
            wait_tunnel_ready(tunnel_url=config.get_url(),
                              require_min_connections=1)
            cfd_cli = CloudflaredCli(config, config_path, LOGGER)
            url = cfd_cli.get_management_wsurl("logs", config, config_path)
            async with connect(url, open_timeout=5, close_timeout=5) as websocket:
                # send start_streaming with info logs only
                await websocket.send(json.dumps({
                    "type": "start_streaming",
                    "filters": {
                        "events": ["tcp"]
                    }
                }))
                # don't expect any http logs
                await generate_and_validate_no_log_event(websocket, config.get_url())
                # send stop_streaming
                await websocket.send('{"type": "stop_streaming"}')


# Every http request has two log lines sent
async def generate_and_validate_log_event(websocket: WebSocketClientProtocol, url: str):
    send_request(url)
    req_line = await websocket.recv()
    log_line = json.loads(req_line)
    assert log_line["type"] == "logs"
    assert log_line["logs"][0]["event"] == "http"
    req_line = await websocket.recv()
    log_line = json.loads(req_line)
    assert log_line["type"] == "logs"
    assert log_line["logs"][0]["event"] == "http"

# Every http request has two log lines sent
async def generate_and_validate_no_log_event(websocket: WebSocketClientProtocol, url: str):
    send_request(url)
    try:
        # wait for 5 seconds and make sure we hit the timeout and not recv any events
        req_line = await asyncio.wait_for(websocket.recv(), 5)
        assert req_line == None, "expected no logs for the specified filters"
    except asyncio.TimeoutError:
        pass

@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def send_request(url, headers={}):
    with requests.Session() as s:
        resp = s.get(url, timeout=BACKOFF_SECS, headers=headers)
        assert resp.status_code == 200, f"{url} returned {resp}"
        return resp.status_code == 200