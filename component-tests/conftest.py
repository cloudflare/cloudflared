import os
import pytest
import yaml

from time import sleep

from config import NamedTunnelConfig, ClassicTunnelConfig
from constants import BACKOFF_SECS
from util import LOGGER


@pytest.fixture(scope="session")
def component_tests_config():
    config_file = os.getenv("COMPONENT_TESTS_CONFIG")
    if config_file is None:
        raise Exception(
            "Need to provide path to config file in COMPONENT_TESTS_CONFIG")
    with open(config_file, 'r') as stream:
        config = yaml.safe_load(stream)
        LOGGER.info(f"component tests base config {config}")

        def _component_tests_config(additional_config={}, named_tunnel=True):

            # Regression test for TUN-4177, running with proxy-dns should not prevent tunnels from running
            additional_config["proxy-dns"] = True
            additional_config["proxy-dns-port"] = 9053

            if named_tunnel:
                return NamedTunnelConfig(additional_config=additional_config,
                                         cloudflared_binary=config['cloudflared_binary'],
                                         tunnel=config['tunnel'],
                                         credentials_file=config['credentials_file'],
                                         ingress=config['ingress'])

            return ClassicTunnelConfig(
                additional_config=additional_config, cloudflared_binary=config['cloudflared_binary'],
                hostname=config['classic_hostname'], origincert=config['origincert'])

        return _component_tests_config


# This fixture is automatically called before each tests to make sure the previous cloudflared has been shutdown
@pytest.fixture(autouse=True)
def wait_previous_cloudflared():
    sleep(BACKOFF_SECS)
