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

    def test_allowed_mail_flag_is_public(self, tmp_path, component_tests_config):
        config = component_tests_config(cfd_mode=CfdModes.QUICK)
        LOGGER.debug(config)
        result = start_cloudflared(
            tmp_path,
            config,
            cfd_pre_args=["tunnel"],
            cfd_args=["--help"],
        )
        output = result.stdout.decode("utf-8", errors="replace")
        LOGGER.debug(output)
        assert "--allowed-mail" in output, \
            f"Expected --allowed-mail in tunnel help, got output:\n{output}"

    def test_quick_tunnel_validates_allowed_mail(self, tmp_path, component_tests_config):
        config = component_tests_config(cfd_mode=CfdModes.QUICK)
        LOGGER.debug(config)
        result = start_cloudflared(
            tmp_path,
            config,
            cfd_pre_args=["tunnel", "--ha-connections", "1"],
            cfd_args=["--hello-world", "--allowed-mail", "invalid"],
            new_process=False,
            expect_success=False,
        )
        stderr = result.stderr.decode("utf-8", errors="replace")
        LOGGER.debug(stderr)
        assert result.returncode != 0, \
            "Expected invalid --allowed-mail to fail, but cloudflared exited successfully"
        assert "is not a valid email address" in stderr, \
            f"Expected --allowed-mail to be validated, got stderr:\n{stderr}"
