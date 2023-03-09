import os
from enum import Enum, auto
from time import sleep

import pytest
import yaml

from config import NamedTunnelConfig, ProxyDnsConfig, QuickTunnelConfig
from constants import BACKOFF_SECS, PROXY_DNS_PORT
from util import LOGGER


class CfdModes(Enum):
    NAMED = auto()
    QUICK = auto()
    PROXY_DNS = auto()


@pytest.fixture(scope="session")
def component_tests_config():
    config_file = os.getenv("COMPONENT_TESTS_CONFIG")
    if config_file is None:
        raise Exception(
            "Need to provide path to config file in COMPONENT_TESTS_CONFIG")
    with open(config_file, 'r') as stream:
        config = yaml.safe_load(stream)
        LOGGER.info(f"component tests base config {config}")

        def _component_tests_config(additional_config={}, cfd_mode=CfdModes.NAMED, run_proxy_dns=True, provide_ingress=True):
            if run_proxy_dns:
                # Regression test for TUN-4177, running with proxy-dns should not prevent tunnels from running.
                # So we run all tests with it.
                additional_config["proxy-dns"] = True
                additional_config["proxy-dns-port"] = PROXY_DNS_PORT
            else:
                additional_config.pop("proxy-dns", None)
                additional_config.pop("proxy-dns-port", None)

            # Allows the ingress rules to be omitted from the provided config
            ingress = []
            if provide_ingress:
                ingress = config['ingress']

            # Provide the hostname to allow routing to the tunnel even if the ingress rule isn't defined in the config
            hostname = config['ingress'][0]['hostname']

            if cfd_mode is CfdModes.NAMED:
                return NamedTunnelConfig(additional_config=additional_config,
                                         cloudflared_binary=config['cloudflared_binary'],
                                         tunnel=config['tunnel'],
                                         credentials_file=config['credentials_file'],
                                         ingress=ingress,
                                         hostname=hostname)
            elif cfd_mode is CfdModes.PROXY_DNS:
                return ProxyDnsConfig(cloudflared_binary=config['cloudflared_binary'])
            elif cfd_mode is CfdModes.QUICK:
                return QuickTunnelConfig(additional_config=additional_config, cloudflared_binary=config['cloudflared_binary'])
            else:
                raise Exception(f"Unknown cloudflared mode {cfd_mode}")

        return _component_tests_config


# This fixture is automatically called before each tests to make sure the previous cloudflared has been shutdown
@pytest.fixture(autouse=True)
def wait_previous_cloudflared():
    sleep(BACKOFF_SECS)
