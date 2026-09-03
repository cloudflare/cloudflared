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

    def test_quick_tunnel_with_allowed_mail_flag_is_rejected(self, tmp_path, component_tests_config):
        """
        Protected quick tunnels are intentionally dead code in this MR: the
        --allowed-mail flag is not registered yet, so passing it must be
        rejected by the CLI and must not create a quick tunnel. This test
        documents that behavior until TUN-10798 registers the flag.
        """
        config = component_tests_config(cfd_mode=CfdModes.QUICK)
        LOGGER.debug(config)
        result = start_cloudflared(
            tmp_path,
            config,
            cfd_pre_args=["tunnel", "--ha-connections", "1"],
            cfd_args=["--hello-world", "--allowed-mail", "test@example.com"],
            new_process=False,
            expect_success=False,
        )
        output = result.stdout.decode("utf-8", errors="replace")
        LOGGER.debug(output)
        assert "flag provided but not defined: -allowed-mail" in output, \
            f"Expected --allowed-mail to be rejected, got output:\n{output}"

