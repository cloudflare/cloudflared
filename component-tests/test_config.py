#!/usr/bin/env python
from util import start_cloudflared

class TestConfig:
    # tmp_path is a fixture provides a temporary directory unique to the test invocation
    def test_validate_ingress_rules(self, tmp_path, component_tests_config):
        extra_config = {
            'ingress': [
                {
                    "hostname": "example.com",
                    "service": "https://localhost:8000",
                    "originRequest": {
                        "originServerName": "test.example.com",
                        "caPool": "/etc/certs/ca.pem"
                    },
                },
                {
                    "hostname": "api.example.com",
                    "path": "login",
                    "service": "https://localhost:9000",
                },
                {
                    "hostname": "wss.example.com",
                    "service": "wss://localhost:8000",
                },
                {
                    "hostname": "ssh.example.com",
                    "service": "ssh://localhost:8000",
                },
                {"service": "http_status:404"}
            ],
        }
        component_tests_config = component_tests_config(extra_config)
        validate_args = ["ingress", "validate"]
        pre_args = ["tunnel"]
        validate = start_cloudflared(tmp_path, component_tests_config, validate_args, pre_args)
        assert validate.returncode == 0, "failed to validate ingress" + validate.stderr.decode("utf-8")

        self.match_rule(tmp_path, component_tests_config, "http://example.com/index.html", 1)
        self.match_rule(tmp_path, component_tests_config, "https://example.com/index.html", 1)
        self.match_rule(tmp_path, component_tests_config, "https://api.example.com/login", 2)
        self.match_rule(tmp_path, component_tests_config, "https://wss.example.com", 3)
        self.match_rule(tmp_path, component_tests_config, "https://ssh.example.com", 4)
        self.match_rule(tmp_path, component_tests_config, "https://api.example.com", 5)


    # This is used to check that the command tunnel ingress url <url> matches rule number <rule_num>. Note that rule number uses 1-based indexing
    def match_rule(self, tmp_path, component_tests_config, url, rule_num):
        args = ["ingress", "rule", url]
        pre_args = ["tunnel"]
        match_rule = start_cloudflared(tmp_path, component_tests_config, args, pre_args)

        assert match_rule.returncode == 0, "failed to check rule" + match_rule.stderr.decode("utf-8")
        assert f"Matched rule #{rule_num}" .encode() in match_rule.stdout