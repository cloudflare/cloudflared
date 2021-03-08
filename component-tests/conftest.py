import os
import pytest
import yaml

from config import ComponentTestConfig, NamedTunnelBaseConfig, ClassicTunnelBaseConfig
from util import LOGGER

@pytest.fixture(scope="session")
def component_tests_config():
    config_file = os.getenv("COMPONENT_TESTS_CONFIG")
    if config_file is None:
        raise Exception("Need to provide path to config file in COMPONENT_TESTS_CONFIG")
    with open(config_file, 'r') as stream:
        config = yaml.safe_load(stream)
        LOGGER.info(f"component tests base config {config}")
        base_named_tunnel_config = NamedTunnelBaseConfig(tunnel=config['tunnel'], credentials_file=config['credentials_file'])
        base_classic_tunnel_config = ClassicTunnelBaseConfig(hostname=config['classic_hostname'], origincert=config['origincert'])

        def _component_tests_config(extra_named_tunnel_config={}, extra_classic_tunnel_config={}):
            named_tunnel_config = base_named_tunnel_config.merge_config(extra_named_tunnel_config)
            classic_tunnel_config = base_classic_tunnel_config.merge_config(extra_classic_tunnel_config)
            return ComponentTestConfig(config['cloudflared_binary'], named_tunnel_config, classic_tunnel_config)

        return _component_tests_config