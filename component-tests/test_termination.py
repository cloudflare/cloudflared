#!/usr/bin/env python
from contextlib import contextmanager
import platform
import signal
import threading
import time

import pytest
import requests

from util import start_cloudflared, wait_tunnel_ready, check_tunnel_not_connected


def supported_signals():
    if platform.system() == "Windows":
        return [signal.SIGTERM]
    return [signal.SIGTERM, signal.SIGINT]


class TestTermination():
    grace_period = 5
    timeout = 10
    extra_config = {
        "grace-period": f"{grace_period}s",
    }
    sse_endpoint = "/sse?freq=1s"

    @pytest.mark.parametrize("signal", supported_signals())
    def test_graceful_shutdown(self, tmp_path, component_tests_config, signal):
        config = component_tests_config(self.extra_config)
        with start_cloudflared(
                tmp_path, config, new_process=True, capture_output=False) as cloudflared:
            wait_tunnel_ready(tunnel_url=config.get_url())

            connected = threading.Condition()
            in_flight_req = threading.Thread(
                target=self.stream_request, args=(config, connected, False, ))
            in_flight_req.start()

            with connected:
                connected.wait(self.timeout)
            # Send signal after the SSE connection is established
            self.terminate_by_signal(cloudflared, signal)
            self.wait_eyeball_thread(
                in_flight_req, self.grace_period + self.timeout)

    # test cloudflared terminates before grace period expires when all eyeball
    # connections are drained
    @pytest.mark.parametrize("signal", supported_signals())
    def test_shutdown_once_no_connection(self, tmp_path, component_tests_config, signal):
        config = component_tests_config(self.extra_config)
        with start_cloudflared(
                tmp_path, config, new_process=True, capture_output=False) as cloudflared:
            wait_tunnel_ready(tunnel_url=config.get_url())

            connected = threading.Condition()
            in_flight_req = threading.Thread(
                target=self.stream_request, args=(config, connected, True, ))
            in_flight_req.start()

            with connected:
                connected.wait(self.timeout)
            with self.within_grace_period():
                # Send signal after the SSE connection is established
                self.terminate_by_signal(cloudflared, signal)
                self.wait_eyeball_thread(in_flight_req, self.grace_period)

    @pytest.mark.parametrize("signal", supported_signals())
    def test_no_connection_shutdown(self, tmp_path, component_tests_config, signal):
        config = component_tests_config(self.extra_config)
        with start_cloudflared(
                tmp_path, config, new_process=True, capture_output=False) as cloudflared:
            wait_tunnel_ready(tunnel_url=config.get_url())
            with self.within_grace_period():
                self.terminate_by_signal(cloudflared, signal)

    def terminate_by_signal(self, cloudflared, sig):
        cloudflared.send_signal(sig)
        check_tunnel_not_connected()
        cloudflared.wait()

    def wait_eyeball_thread(self, thread, timeout):
        thread.join(timeout)
        assert thread.is_alive() == False, "eyeball thread is still alive"

    # Using this context asserts logic within the context is executed within grace period
    @contextmanager
    def within_grace_period(self):
        try:
            start = time.time()
            yield
        finally:
            duration = time.time() - start
            assert duration < self.grace_period

    def stream_request(self, config, connected, early_terminate):
        expected_terminate_message = "502 Bad Gateway"
        url = config.get_url() + self.sse_endpoint

        with requests.get(url, timeout=5, stream=True) as resp:
            with connected:
                connected.notifyAll()
            lines = 0
            for line in resp.iter_lines():
                if expected_terminate_message.encode() == line:
                    break
                lines += 1
                if early_terminate and lines == 2:
                    return
            # /sse returns count followed by 2 new lines
            assert lines >= (self.grace_period * 2)
