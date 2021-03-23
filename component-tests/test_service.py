#!/usr/bin/env python
import os
import platform
import subprocess
from contextlib import contextmanager
from pathlib import Path

import pytest

import test_logging
from util import start_cloudflared, wait_tunnel_ready


def select_platform(plat):
    return pytest.mark.skipif(
        platform.system() != plat, reason=f"Only runs on {plat}")


def default_config_dir():
    return os.path.join(Path.home(), ".cloudflared")


def default_config_file():
    return os.path.join(default_config_dir(), "config.yml")


class TestServiceMode:
    @select_platform("Darwin")
    @pytest.mark.skipif(os.path.exists(default_config_file()), reason=f"There is already a config file in default path")
    def test_launchd_service_log_to_file(self, tmp_path, component_tests_config):
        log_file = tmp_path / test_logging.default_log_file
        additional_config = {
            # On Darwin cloudflared service defaults to run classic tunnel command
            "hello-world": True,
            "logfile": str(log_file),
        }
        config = component_tests_config(additional_config=additional_config, named_tunnel=False)

        def assert_log_file():
            test_logging.assert_log_in_file(log_file)
            test_logging.assert_json_log(log_file)

        self.launchd_service_scenario(config, assert_log_file)

    @select_platform("Darwin")
    @pytest.mark.skipif(os.path.exists(default_config_file()), reason=f"There is already a config file in default path")
    def test_launchd_service_rotating_log(self, tmp_path, component_tests_config):
        log_dir = tmp_path / "logs"
        additional_config = {
            # On Darwin cloudflared service defaults to run classic tunnel command
            "hello-world": True,
            "loglevel": "debug",
            "log-directory": str(log_dir),
        }
        config = component_tests_config(additional_config=additional_config, named_tunnel=False)

        def assert_rotating_log():
            test_logging.assert_log_to_dir(config, log_dir)

        self.launchd_service_scenario(config, assert_rotating_log)

    def launchd_service_scenario(self, config, extra_assertions):
        with self.run_service(Path(default_config_dir()), config):
            self.launchctl_cmd("list")
            self.launchctl_cmd("start")
            wait_tunnel_ready(tunnel_url=config.get_url())
            extra_assertions()
            self.launchctl_cmd("stop")

        os.remove(default_config_file())
        self.launchctl_cmd("list", success=False)

    @select_platform("Linux")
    @pytest.mark.skipif(os.path.exists("/etc/cloudflared/config.yml"),
                        reason=f"There is already a config file in default path")
    def test_sysv_service_log_to_file(self, tmp_path, component_tests_config):
        log_file = tmp_path / test_logging.default_log_file
        additional_config = {
            "logfile": str(log_file),
        }
        config = component_tests_config(additional_config=additional_config)

        def assert_log_file():
            test_logging.assert_log_in_file(log_file)
            test_logging.assert_json_log(log_file)

        self.sysv_service_scenario(config, tmp_path, assert_log_file)

    @select_platform("Linux")
    @pytest.mark.skipif(os.path.exists("/etc/cloudflared/config.yml"),
                        reason=f"There is already a config file in default path")
    def test_sysv_service_rotating_log(self, tmp_path, component_tests_config):
        log_dir = tmp_path / "logs"
        additional_config = {
            "loglevel": "debug",
            "log-directory": str(log_dir),
        }
        config = component_tests_config(additional_config=additional_config)

        def assert_rotating_log():
            # We need the folder to have executable permissions for the "stat" command in the assertions to work.
            subprocess.check_call(['sudo', 'chmod', 'o+x', log_dir])
            test_logging.assert_log_to_dir(config, log_dir)

        self.sysv_service_scenario(config, tmp_path, assert_rotating_log)

    def sysv_service_scenario(self, config, tmp_path, extra_assertions):
        with self.run_service(tmp_path, config, root=True):
            self.sysv_cmd("start")
            self.sysv_cmd("status")
            wait_tunnel_ready(tunnel_url=config.get_url())
            extra_assertions()
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
