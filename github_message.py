#!/usr/bin/python3
"""
Create Github Releases Notes with binary checksums from Workers KV
"""

import argparse
import logging
import os
import requests

from github import Github, UnknownObjectException

FORMAT = "%(levelname)s - %(asctime)s: %(message)s"
logging.basicConfig(format=FORMAT, level=logging.INFO)

CLOUDFLARED_REPO = os.environ.get("GITHUB_REPO", "cloudflare/cloudflared")
GITHUB_CONFLICT_CODE = "already_exists"
BASE_KV_URL = 'https://api.cloudflare.com/client/v4/accounts/'


def kv_get_keys(prefix, account, namespace, api_token):
    """ get the KV keys for a given prefix """
    response = requests.get(
        BASE_KV_URL + account + "/storage/kv/namespaces/" +
        namespace + "/keys" + "?prefix=" + prefix,
        headers={
            "Content-Type": "application/json",
            "Authorization": "Bearer " + api_token,
        },
    )
    if response.status_code != 200:
        jsonResponse = response.json()
        errors = jsonResponse["errors"]
        if len(errors) > 0:
            raise Exception("failed to get checksums: {0}", errors[0])
    return response.json()["result"]


def kv_get_value(key, account, namespace, api_token):
    """ get the KV value for a provided key """
    response = requests.get(
        BASE_KV_URL + account + "/storage/kv/namespaces/" + namespace + "/values/" + key,
        headers={
            "Content-Type": "application/json",
            "Authorization": "Bearer " + api_token,
        },
    )
    if response.status_code != 200:
        jsonResponse = response.json()
        errors = jsonResponse["errors"]
        if len(errors) > 0:
            raise Exception("failed to get checksums: {0}", errors[0])
    return response.text


def update_or_add_message(msg, name, sha):
    """ 
    updates or builds the github version message for each new asset's sha256. 
    Searches the existing message string to update or create. 
    """
    new_text = '{0}: {1}\n'.format(name, sha)
    start = msg.find(name)
    if (start != -1):
        end = msg.find("\n", start)
        if (end != -1):
            return msg.replace(msg[start:end+1], new_text)
    back = msg.rfind("```")
    if (back != -1):
        return '{0}{1}```'.format(msg[:back], new_text)
    return '{0} \n### SHA256 Checksums:\n```\n{1}```'.format(msg, new_text)


def get_release(repo, version):
    """ Get a Github Release matching the version tag. """
    try:
        release = repo.get_release(version)
        logging.info("Release %s found", version)
        return release
    except UnknownObjectException:
        logging.info("Release %s not found", version)


def parse_args():
    """ Parse and validate args """
    parser = argparse.ArgumentParser(
        description="Updates a Github Release with checksums from KV"
    )
    parser.add_argument(
        "--api-key", default=os.environ.get("API_KEY"), help="Github API key"
    )
    parser.add_argument(
        "--kv-namespace-id", default=os.environ.get("KV_NAMESPACE"), help="workers KV namespace id"
    )
    parser.add_argument(
        "--kv-account-id", default=os.environ.get("KV_ACCOUNT"), help="workers KV account id"
    )
    parser.add_argument(
        "--kv-api-token", default=os.environ.get("KV_API_TOKEN"), help="workers KV API Token"
    )
    parser.add_argument(
        "--release-version",
        metavar="version",
        default=os.environ.get("VERSION"),
        help="Release version",
    )
    parser.add_argument(
        "--dry-run", action="store_true", help="Do not modify the release message"
    )

    args = parser.parse_args()
    is_valid = True
    if not args.release_version:
        logging.error("Missing release version")
        is_valid = False

    if not args.api_key:
        logging.error("Missing API key")
        is_valid = False

    if not args.kv_namespace_id:
        logging.error("Missing KV namespace id")
        is_valid = False

    if not args.kv_account_id:
        logging.error("Missing KV account id")
        is_valid = False

    if not args.kv_api_token:
        logging.error("Missing KV API token")
        is_valid = False

    if is_valid:
        return args

    parser.print_usage()
    exit(1)


def main():
    """ Attempts to update the Github Release message with the github asset's checksums """
    try:
        args = parse_args()
        client = Github(args.api_key)
        repo = client.get_repo(CLOUDFLARED_REPO)
        release = get_release(repo, args.release_version)

        msg = ""

        prefix = f"update_{args.release_version}_"
        keys = kv_get_keys(prefix, args.kv_account_id,
                           args.kv_namespace_id, args.kv_api_token)
        for key in [k["name"] for k in keys]:
            checksum = kv_get_value(
                key, args.kv_account_id, args.kv_namespace_id, args.kv_api_token)
            binary_name = key[len(prefix):]
            msg = update_or_add_message(msg, binary_name, checksum)

        if args.dry_run:
            logging.info("Skipping release message update because of dry-run")
            logging.info(f"Github message:\n{msg}")
            return

        # update the release body text
        release.update_release(args.release_version, msg)

    except Exception as e:
        logging.exception(e)
        exit(1)


main()
