import os
import pytest
import yaml

from time import sleep

from config import ComponentTestConfig, NamedTunnelConfig, ClassicTunnelConfig
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

        def _component_tests_config(extra_named_tunnel_config={}, extra_classic_tunnel_config={}):
            named_tunnel_config = NamedTunnelConfig(additional_config=extra_named_tunnel_config,
                                                    tunnel=config['tunnel'], credentials_file=config['credentials_file'],  ingress=config['ingress'])
            classic_tunnel_config = ClassicTunnelConfig(
                additional_config=extra_classic_tunnel_config, hostname=config['classic_hostname'], origincert=config['origincert'])
            return ComponentTestConfig(config['cloudflared_binary'], named_tunnel_config, classic_tunnel_config)

        return _component_tests_config


# This fixture is automatically called before each tests to make sure the previous cloudflared has been shutdown
@pytest.fixture(autouse=True)
def wait_previous_cloudflared():
    sleep(BACKOFF_SECS)
