#!/usr/bin/env python
from contextlib import contextmanager
import os
from pathlib import Path
import platform
import pytest
import subprocess

from util import start_cloudflared, cloudflared_cmd, wait_tunnel_ready, LOGGER


def select_platform(plat):
    return pytest.mark.skipif(
        platform.system() != plat, reason=f"Only runs on {plat}")


def default_config_dir():
    return os.path.join(Path.home(), ".cloudflared")


def default_config_file():
    return os.path.join(default_config_dir(), "config.yml")


class TestServiceMode():
    @select_platform("Darwin")
    @pytest.mark.skipif(os.path.exists(default_config_file()), reason=f"There is already a config file in default path")
    def test_launchd_service(self, component_tests_config):
        # On Darwin cloudflared service defaults to run classic tunnel command
        additional_config = {
            "hello-world": True,
        }
        config = component_tests_config(
            additional_config=additional_config, named_tunnel=False)
        with self.run_service(Path(default_config_dir()), config):
            self.launchctl_cmd("list")
            self.launchctl_cmd("start")
            wait_tunnel_ready(tunnel_url=config.get_url())
            self.launchctl_cmd("stop")

        os.remove(default_config_file())
        self.launchctl_cmd("list", success=False)

    @select_platform("Linux")
    @pytest.mark.skipif(os.path.exists("/etc/cloudflared/config.yml"), reason=f"There is already a config file in default path")
    def test_sysv_service(self, tmp_path, component_tests_config):
        config = component_tests_config()
        with self.run_service(tmp_path, config, root=True):
            self.sysv_cmd("start")
            self.sysv_cmd("status")
            wait_tunnel_ready(tunnel_url=config.get_url())
            self.sysv_cmd("stop")
        # Service install copies config file to /etc/cloudflared/config.yml
        subprocess.run(["sudo", "rm", "/etc/cloudflared/config.yml"])
        self.sysv_cmd("status", success=False)

    @contextmanager
    def run_service(self, tmp_path, config, root=False):
        try:
            service = start_cloudflared(
                tmp_path, config, cfd_args=["service", "install"], cfd_pre_args=[], capture_output=False, root=root)
            yield service
        finally:
            start_cloudflared(
                tmp_path, config, cfd_args=["service", "uninstall"], cfd_pre_args=[], capture_output=False, root=root)

    def launchctl_cmd(self, action, success=True):
        cmd = subprocess.run(
            ["launchctl", action, "com.cloudflare.cloudflared"], check=success)
        if not success:
            assert cmd.returncode != 0, f"Expect {cmd.args} to fail, but it succeed"

    def sysv_cmd(self, action, success=True):
        cmd = subprocess.run(
            ["sudo", "service", "cloudflared", action], check=success)
        if not success:
            assert cmd.returncode != 0, f"Expect {cmd.args} to fail, but it succeed"
