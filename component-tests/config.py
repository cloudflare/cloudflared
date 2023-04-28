#!/usr/bin/env python
import copy
import json
import base64

from dataclasses import dataclass, InitVar

from constants import METRICS_PORT, PROXY_DNS_PORT

# frozen=True raises exception when assigning to fields. This emulates immutability


@dataclass(frozen=True)
class BaseConfig:
    cloudflared_binary: str
    no_autoupdate: bool = True
    metrics: str = f'localhost:{METRICS_PORT}'

    def merge_config(self, additional):
        config = copy.copy(additional)
        config['no-autoupdate'] = self.no_autoupdate
        config['metrics'] = self.metrics
        return config


@dataclass(frozen=True)
class NamedTunnelBaseConfig(BaseConfig):
    # The attributes of the parent class are ordered before attributes in this class,
    # so we have to use default values here and check if they are set in __post_init__
    tunnel: str = None
    credentials_file: str = None
    ingress: list = None
    hostname: str = None

    def __post_init__(self):
        if self.tunnel is None:
            raise TypeError("Field tunnel is not set")
        if self.credentials_file is None:
            raise TypeError("Field credentials_file is not set")
        if self.ingress is None:
            raise TypeError("Field ingress is not set")

    def merge_config(self, additional):
        config = super(NamedTunnelBaseConfig, self).merge_config(additional)
        if 'tunnel' not in config:
            config['tunnel'] = self.tunnel
        if 'credentials-file' not in config:
            config['credentials-file'] = self.credentials_file
        # In some cases we want to override default ingress, such as in config tests
        if 'ingress' not in config:
            config['ingress'] = self.ingress
        return config


@dataclass(frozen=True)
class NamedTunnelConfig(NamedTunnelBaseConfig):
    full_config: dict = None
    additional_config: InitVar[dict] = {}

    def __post_init__(self, additional_config):
        # Cannot call set self.full_config because the class is frozen, instead, we can use __setattr__
        # https://docs.python.org/3/library/dataclasses.html#frozen-instances
        object.__setattr__(self, 'full_config',
                           self.merge_config(additional_config))

    def get_url(self):
        return "https://" + self.hostname

    def base_config(self):
        config = self.full_config.copy()

        # removes the tunnel reference
        del(config["tunnel"])
        del(config["credentials-file"])

        return config

    def get_tunnel_id(self):
        return self.full_config["tunnel"]

    def get_token(self):
        creds = self.get_credentials_json()
        token_dict = {"a": creds["AccountTag"], "t": creds["TunnelID"], "s": creds["TunnelSecret"]}
        token_json_str = json.dumps(token_dict)
        return base64.b64encode(token_json_str.encode('utf-8'))

    def get_credentials_json(self):
        with open(self.credentials_file) as json_file:
            return json.load(json_file)
        
@dataclass(frozen=True)
class QuickTunnelConfig(BaseConfig):
    full_config: dict = None
    additional_config: InitVar[dict] = {}

    def __post_init__(self, additional_config):
        # Cannot call set self.full_config because the class is frozen, instead, we can use __setattr__
        # https://docs.python.org/3/library/dataclasses.html#frozen-instances
        object.__setattr__(self, 'full_config',
                           self.merge_config(additional_config))

@dataclass(frozen=True)
class ProxyDnsConfig(BaseConfig):
    full_config = {
        "port": PROXY_DNS_PORT,
        "no-autoupdate": True,
    }

