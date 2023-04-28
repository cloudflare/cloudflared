#!/usr/bin/env python
import socket
from time import sleep

import constants
from conftest import CfdModes
from util import start_cloudflared, wait_tunnel_ready, check_tunnel_not_connected


# Sanity checks that test that we only run Proxy DNS and Tunnel when we really expect them to be there.
class TestProxyDns:
    def test_proxy_dns_with_named_tunnel(self, tmp_path, component_tests_config):
        run_test_scenario(tmp_path, component_tests_config, CfdModes.NAMED, run_proxy_dns=True)

    def test_proxy_dns_alone(self, tmp_path, component_tests_config):
        run_test_scenario(tmp_path, component_tests_config, CfdModes.PROXY_DNS, run_proxy_dns=True)

    def test_named_tunnel_alone(self, tmp_path, component_tests_config):
        run_test_scenario(tmp_path, component_tests_config, CfdModes.NAMED, run_proxy_dns=False)


def run_test_scenario(tmp_path, component_tests_config, cfd_mode, run_proxy_dns):
    expect_proxy_dns = run_proxy_dns
    expect_tunnel = False

    if cfd_mode == CfdModes.NAMED:
        expect_tunnel = True
        pre_args = ["tunnel", "--ha-connections", "1"]
        args = ["run"]
    elif cfd_mode == CfdModes.PROXY_DNS:
        expect_proxy_dns = True
        pre_args = []
        args = ["proxy-dns", "--port", str(constants.PROXY_DNS_PORT)]
    else:
        assert False, f"Unknown cfd_mode {cfd_mode}"

    config = component_tests_config(cfd_mode=cfd_mode, run_proxy_dns=run_proxy_dns)
    with start_cloudflared(tmp_path, config, cfd_pre_args=pre_args, cfd_args=args, new_process=True, capture_output=False):
        if expect_tunnel:
            wait_tunnel_ready()
        else:
            check_tunnel_not_connected()
        verify_proxy_dns(expect_proxy_dns)


def verify_proxy_dns(should_be_running):
    # Wait for the Proxy DNS listener to come up.
    sleep(constants.BACKOFF_SECS)
    had_failure = False
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        sock.connect(('localhost', constants.PROXY_DNS_PORT))
        sock.send(b"anything")
    except:
        if should_be_running:
            assert False, "Expected Proxy DNS to be running, but it was not."
        had_failure = True
    finally:
        sock.close()

    if not should_be_running and not had_failure:
        assert False, "Proxy DNS should not have been running, but it was."
