import logging
import os
import subprocess
import yaml

LOGGER = logging.getLogger(__name__)

def write_config(path, config):
    config_path = path / "config.yaml"
    with open(config_path, 'w') as outfile:
        yaml.dump(config, outfile)
    return config_path


def start_cloudflared(path, component_test_config, cfd_args, cfd_pre_args=[], classic=False):
    if classic:
        config = component_test_config.classic_tunnel_config
    else:
        config = component_test_config.named_tunnel_config
    config_path = write_config(path, config)
    cmd = [component_test_config.cloudflared_binary]
    cmd += cfd_pre_args
    cmd += ["--config", config_path]
    cmd += cfd_args
    LOGGER.info(f"Run cmd {cmd} with config {config}")
    return subprocess.run(cmd, capture_output=True)