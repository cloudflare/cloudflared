#!/usr/bin/env python
import json
import os

from constants import MAX_LOG_LINES
from util import start_cloudflared, wait_tunnel_ready, send_requests

# Rolling logger rotate log files after 1 MB
rotate_after_size = 1000 * 1000
default_log_file = "cloudflared.log"
expect_message = "Starting Hello"


def assert_log_to_terminal(cloudflared):
    for _ in range(0, MAX_LOG_LINES):
        line = cloudflared.stderr.readline()
        if not line:
            break
        if expect_message.encode() in line:
            return
    raise Exception(f"terminal log doesn't contain {expect_message}")


def assert_log_in_file(file):
    with open(file, "r") as f:
        for _ in range(0, MAX_LOG_LINES):
            line = f.readline()
            if not line:
                break
            if expect_message in line:
                return
    raise Exception(f"log file doesn't contain {expect_message}")


def assert_json_log(file):
    def assert_in_json(j, key):
        assert key in j, f"{key} is not in j"

    with open(file, "r") as f:
        line = f.readline()
        json_log = json.loads(line)
        assert_in_json(json_log, "level")
        assert_in_json(json_log, "time")
        assert_in_json(json_log, "message")


def assert_log_to_dir(config, log_dir):
    max_batches = 3
    batch_requests = 1000
    for _ in range(max_batches):
        send_requests(config.get_url(),
                      batch_requests, require_ok=False)
        files = os.listdir(log_dir)
        if len(files) == 2:
            current_log_file_index = files.index(default_log_file)
            current_file = log_dir / files[current_log_file_index]
            stats = os.stat(current_file)
            assert stats.st_size > 0
            assert_json_log(current_file)

            # One file is the current log file, the other is the rotated log file
            rotated_log_file_index = 0 if current_log_file_index == 1 else 1
            rotated_file = log_dir / files[rotated_log_file_index]
            stats = os.stat(rotated_file)
            assert stats.st_size > rotate_after_size
            assert_log_in_file(rotated_file)
            assert_json_log(current_file)
            return

    raise Exception(
        f"Log file isn't rotated after sending {max_batches * batch_requests} requests")


class TestLogging:
    def test_logging_to_terminal(self, tmp_path, component_tests_config):
        config = component_tests_config()
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1"], new_process=True) as cloudflared:
            wait_tunnel_ready(tunnel_url=config.get_url())
            assert_log_to_terminal(cloudflared)

    def test_logging_to_file(self, tmp_path, component_tests_config):
        log_file = tmp_path / default_log_file
        extra_config = {
            # Convert from pathlib.Path to str
            "logfile": str(log_file),
        }
        config = component_tests_config(extra_config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1"], new_process=True, capture_output=False):
            wait_tunnel_ready(tunnel_url=config.get_url(), cfd_logs=str(log_file))
            assert_log_in_file(log_file)
            assert_json_log(log_file)

    def test_logging_to_dir(self, tmp_path, component_tests_config):
        log_dir = tmp_path / "logs"
        extra_config = {
            "loglevel": "debug",
            # Convert from pathlib.Path to str
            "log-directory": str(log_dir),
        }
        config = component_tests_config(extra_config)
        with start_cloudflared(tmp_path, config, cfd_pre_args=["tunnel", "--ha-connections", "1"], new_process=True, capture_output=False):
            wait_tunnel_ready(tunnel_url=config.get_url(), cfd_logs=str(log_dir))
            assert_log_to_dir(config, log_dir)
