from util import LOGGER, nofips, start_cloudflared, wait_tunnel_ready


@nofips
class TestPostQuantum:
    def _extra_config(self):
        config = {
            "protocol": "quic",
        }
        return config

    def test_post_quantum(self, tmp_path, component_tests_config):
        config = component_tests_config(self._extra_config())
        LOGGER.debug(config)
        with start_cloudflared(tmp_path, config, cfd_args=["run", "--post-quantum"], new_process=True):
            wait_tunnel_ready(tunnel_url=config.get_url(),
                              require_min_connections=4)
