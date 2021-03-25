#!/usr/bin/env python
import copy

from dataclasses import dataclass, InitVar

from constants import METRICS_PORT

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

    def __post_init__(self):
        if self.tunnel is None:
            raise TypeError("Field tunnel is not set")
        if self.credentials_file is None:
            raise TypeError("Field credentials_file is not set")
        if self.ingress is None:
            raise TypeError("Field ingress is not set")

    def merge_config(self, additional):
        config = super(NamedTunnelBaseConfig, self).merge_config(additional)
        config['tunnel'] = self.tunnel
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
        return "https://" + self.ingress[0]['hostname']


@dataclass(frozen=True)
class ClassicTunnelBaseConfig(BaseConfig):
    hostname: str = None
    origincert: str = None

    def __post_init__(self):
        if self.hostname is None:
            raise TypeError("Field tunnel is not set")
        if self.origincert is None:
            raise TypeError("Field credentials_file is not set")

    def merge_config(self, additional):
        config = super(ClassicTunnelBaseConfig, self).merge_config(additional)
        config['hostname'] = self.hostname
        config['origincert'] = self.origincert
        return config


@dataclass(frozen=True)
class ClassicTunnelConfig(ClassicTunnelBaseConfig):
    full_config: dict = None
    additional_config: InitVar[dict] = {}

    def __post_init__(self, additional_config):
        # Cannot call set self.full_config because the class is frozen, instead, we can use __setattr__
        # https://docs.python.org/3/library/dataclasses.html#frozen-instances
        object.__setattr__(self, 'full_config',
                           self.merge_config(additional_config))

    def get_url(self):
        return "https://" + self.hostname
