import logging
import os
import subprocess
import yaml

LOGGER = logging.getLogger(__name__)

def get_cloudflared():
    cfd_binary = os.getenv('CFD_BINARY')
    return "cloudflared" if cfd_binary is None else cfd_binary


def write_config(path, config):
    config_path = path / "config.yaml"
    with open(config_path, 'w') as outfile:
        yaml.dump(config, outfile)
    return config_path


def start_cloudflared(path, config, args, pre_args=[]):
    config_path = write_config(path, config)
    cmd = [get_cloudflared()]
    cmd += pre_args
    cmd += ["--config", config_path]
    cmd += args
    LOGGER.info(f"Run cmd {cmd} with config {config}")
    return subprocess.run(cmd, capture_output=True)