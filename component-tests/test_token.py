import base64
import json

from setup import get_config_from_file
from util import start_cloudflared


class TestToken:
    def test_get_token(self, tmp_path, component_tests_config):
        config = component_tests_config()
        tunnel_id = config.get_tunnel_id()

        token_args = ["--origincert", cert_path(), "token", tunnel_id]
        output = start_cloudflared(tmp_path, config, token_args)

        assert parse_token(config.get_token()) == parse_token(output.stdout)

    def test_get_credentials_file(self, tmp_path, component_tests_config):
        config = component_tests_config()
        tunnel_id = config.get_tunnel_id()

        cred_file = tmp_path / "test_get_credentials_file.json"
        token_args = ["--origincert", cert_path(), "token", "--cred-file", cred_file, tunnel_id]
        start_cloudflared(tmp_path, config, token_args)

        with open(cred_file) as json_file:
            assert config.get_credentials_json() == json.load(json_file)


def cert_path():
    return get_config_from_file()["origincert"]


def parse_token(token):
    return json.loads(base64.b64decode(token))
