#!/usr/bin/env python
import argparse
import base64
import json
import os
import subprocess
import uuid

import CloudFlare
import yaml
from retrying import retry

from constants import MAX_RETRIES, BACKOFF_SECS
from util import LOGGER


def get_config_from_env():
    config_content = base64.b64decode(get_env("COMPONENT_TESTS_CONFIG_CONTENT")).decode('utf-8')
    return yaml.safe_load(config_content)


def get_config_from_file():
    config_path = get_env("COMPONENT_TESTS_CONFIG")
    with open(config_path, 'r') as infile:
        return yaml.safe_load(infile)


def persist_config(config):
    config_path = get_env("COMPONENT_TESTS_CONFIG")
    with open(config_path, 'w') as outfile:
        yaml.safe_dump(config, outfile)


def persist_origin_cert(config):
    origincert = get_env("COMPONENT_TESTS_ORIGINCERT")
    path = config["origincert"]
    with open(path, 'w') as outfile:
        outfile.write(origincert)
    return path


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def create_tunnel(config, origincert_path, random_uuid):
    # Delete any previous existing credentials file. If the agent keeps files around (that's the case in Windows) then
    # cloudflared tunnel create will refuse to create the tunnel because it does not want to overwrite credentials
    # files.
    credentials_path = config["credentials_file"]
    try:
        os.remove(credentials_path)
    except OSError:
        pass

    tunnel_name = "cfd_component_test-" + random_uuid
    create_cmd = [config["cloudflared_binary"], "tunnel", "--origincert", origincert_path, "create",
                  "--credentials-file", credentials_path, tunnel_name]
    LOGGER.info(f"Creating tunnel with {create_cmd}")
    subprocess.run(create_cmd, check=True)

    list_cmd = [config["cloudflared_binary"], "tunnel", "--origincert", origincert_path, "list", "--name",
                tunnel_name, "--output", "json"]
    LOGGER.info(f"Listing tunnel with {list_cmd}")
    cloudflared = subprocess.run(list_cmd, check=True, capture_output=True)
    return json.loads(cloudflared.stdout)[0]["id"]


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def delete_tunnel(config):
    credentials_path = config["credentials_file"]
    delete_cmd = [config["cloudflared_binary"], "tunnel", "--origincert", config["origincert"], "delete",
                  "--credentials-file", credentials_path, "-f", config["tunnel"]]
    LOGGER.info(f"Deleting tunnel with {delete_cmd}")
    subprocess.run(delete_cmd, check=True)


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def create_dns(config, hostname, type, content):
    cf = CloudFlare.CloudFlare(debug=True, token=get_env("DNS_API_TOKEN"))
    cf.zones.dns_records.post(
        config["zone_tag"],
        data={'name': hostname, 'type': type, 'content': content, 'proxied': True}
    )


def create_classic_dns(config, random_uuid):
    classic_hostname = "classic-" + random_uuid + "." + config["zone_domain"]
    create_dns(config, classic_hostname, "AAAA", "fd10:aec2:5dae::")
    return classic_hostname


def create_named_dns(config, random_uuid):
    hostname = "named-" + random_uuid + "." + config["zone_domain"]
    create_dns(config, hostname, "CNAME", config["tunnel"] + ".cfargotunnel.com")
    return hostname


@retry(stop_max_attempt_number=MAX_RETRIES, wait_fixed=BACKOFF_SECS * 1000)
def delete_dns(config, hostname):
    cf = CloudFlare.CloudFlare(debug=True, token=get_env("DNS_API_TOKEN"))
    zone_tag = config["zone_tag"]
    dns_records = cf.zones.dns_records.get(zone_tag, params={'name': hostname})
    if len(dns_records) > 0:
        cf.zones.dns_records.delete(zone_tag, dns_records[0]['id'])


def write_file(content, path):
    with open(path, 'w') as outfile:
        outfile.write(content)


def get_env(env_name):
    val = os.getenv(env_name)
    if val is None:
        raise Exception(f"{env_name} is not set")
    return val


def create():
    """
    Creates the necessary resources for the components test to run.
     - Creates a named tunnel with a random name.
     - Creates a random CNAME DNS entry for that tunnel.
     - Creates a random AAAA DNS entry for a classic tunnel.

    Those created resources are added to the config (obtained from an environment variable).
    The resulting configuration is persisted for the tests to use.
    """
    config = get_config_from_env()
    origincert_path = persist_origin_cert(config)

    random_uuid = str(uuid.uuid4())
    config["tunnel"] = create_tunnel(config, origincert_path, random_uuid)
    config["classic_hostname"] = create_classic_dns(config, random_uuid)
    config["ingress"] = [
        {
            "hostname": create_named_dns(config, random_uuid),
            "service": "hello_world"
        },
        {
            "service": "http_status:404"
        }
    ]

    persist_config(config)


def cleanup():
    """
    Reads the persisted configuration that was created previously.
    Deletes the resources that were created there.
    """
    config = get_config_from_file()
    delete_tunnel(config)
    delete_dns(config, config["classic_hostname"])
    delete_dns(config, config["ingress"][0]["hostname"])


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='setup component tests')
    parser.add_argument('--type', choices=['create', 'cleanup'], default='create')
    args = parser.parse_args()

    if args.type == 'create':
        create()
    else:
        cleanup()
