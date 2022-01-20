#!/usr/bin/python3
"""
Creates Github Releases and uploads assets
"""

import argparse
import logging
import os
import shutil
import hashlib
import requests
import tarfile
from os import listdir
from os.path import isfile, join
import re

from github import Github, GithubException, UnknownObjectException

FORMAT = "%(levelname)s - %(asctime)s: %(message)s"
logging.basicConfig(format=FORMAT)

CLOUDFLARED_REPO = os.environ.get("GITHUB_REPO", "cloudflare/cloudflared")
GITHUB_CONFLICT_CODE = "already_exists"
BASE_KV_URL = 'https://api.cloudflare.com/client/v4/accounts/'
UPDATER_PREFIX = 'update'

def get_sha256(filename):
    """ get the sha256 of a file """
    sha256_hash = hashlib.sha256()
    with open(filename,"rb") as f:
        for byte_block in iter(lambda: f.read(4096),b""):
            sha256_hash.update(byte_block)
        return sha256_hash.hexdigest()

def send_hash(pkg_hash, name, version, account, namespace, api_token):
    """ send the checksum of a file to workers kv """
    key = '{0}_{1}_{2}'.format(UPDATER_PREFIX, version, name)
    headers = {
        "Content-Type": "application/json",
        "Authorization": "Bearer " + api_token,
    }
    response = requests.put(
            BASE_KV_URL + account + "/storage/kv/namespaces/" + namespace + "/values/" + key,
            headers=headers,
            data=pkg_hash
    )

    if response.status_code != 200:
        jsonResponse = response.json()
        errors = jsonResponse["errors"]
        if len(errors) > 0:
            raise Exception("failed to upload checksum: {0}", errors[0])



def assert_tag_exists(repo, version):
    """ Raise exception if repo does not contain a tag matching version """
    tags = repo.get_tags()
    if not tags or tags[0].name != version:
        raise Exception("Tag {} not found".format(version))


def get_or_create_release(repo, version, dry_run=False):
    """
    Get a Github Release matching the version tag or create a new one.
    If a conflict occurs on creation, attempt to fetch the Release on last time
    """
    try:
        release = repo.get_release(version)
        logging.info("Release %s found", version)
        return release
    except UnknownObjectException:
        logging.info("Release %s not found", version)

    # We don't want to create a new release tag if one doesn't already exist
    assert_tag_exists(repo, version)

    if dry_run:
        logging.info("Skipping Release creation because of dry-run")
        return

    try:
        logging.info("Creating release %s", version)
        return repo.create_git_release(version, version, "")
    except GithubException as e:
        errors = e.data.get("errors", [])
        if e.status == 422 and any(
            [err.get("code") == GITHUB_CONFLICT_CODE for err in errors]
        ):
            logging.warning(
                "Conflict: Release was likely just made by a different build: %s",
                e.data,
            )
            return repo.get_release(version)
        raise e


def parse_args():
    """ Parse and validate args """
    parser = argparse.ArgumentParser(
        description="Creates Github Releases and uploads assets."
    )
    parser.add_argument(
        "--api-key", default=os.environ.get("API_KEY"), help="Github API key"
    )
    parser.add_argument(
        "--release-version",
        metavar="version",
        default=os.environ.get("VERSION"),
        help="Release version",
    )
    parser.add_argument(
        "--path", default=os.environ.get("ASSET_PATH"), help="Asset path"
    )
    parser.add_argument(
        "--name", default=os.environ.get("ASSET_NAME"), help="Asset Name"
    )
    parser.add_argument(
        "--namespace-id", default=os.environ.get("KV_NAMESPACE"), help="workersKV namespace id"
    )
    parser.add_argument(
        "--kv-account-id", default=os.environ.get("KV_ACCOUNT"), help="workersKV account id"
    )
    parser.add_argument(
        "--kv-api-token", default=os.environ.get("KV_API_TOKEN"), help="workersKV API Token"
    )
    parser.add_argument(
        "--dry-run", action="store_true", help="Do not create release or upload asset"
    )

    args = parser.parse_args()
    is_valid = True
    if not args.release_version:
        logging.error("Missing release version")
        is_valid = False

    if not args.path:
        logging.error("Missing asset path")
        is_valid = False

    if not args.name and not os.path.isdir(args.path):
        logging.error("Missing asset name")
        is_valid = False

    if not args.api_key:
        logging.error("Missing API key")
        is_valid = False
    
    if not args.namespace_id:
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

def upload_asset(release, filepath, filename, release_version, kv_account_id, namespace_id, kv_api_token):
    logging.info("Uploading asset: %s", filename)
    release.upload_asset(filepath, name=filename)

    # check and extract if the file is a tar and gzipped file (as is the case with the macos builds)
    binary_path = filepath
    if binary_path.endswith("tgz"):
        try:
            shutil.rmtree('cfd')
        except OSError:
            pass
        zipfile = tarfile.open(binary_path, "r:gz")
        zipfile.extractall('cfd') # specify which folder to extract to
        zipfile.close()

        binary_path = os.path.join(os.getcwd(), 'cfd', 'cloudflared')

    # send the sha256 (the checksum) to workers kv
    pkg_hash = get_sha256(binary_path)
    send_hash(pkg_hash, filename, release_version, kv_account_id, namespace_id, kv_api_token)

    # create the artifacts directory if it doesn't exist
    artifact_path = os.path.join(os.getcwd(), 'artifacts')
    if not os.path.isdir(artifact_path):
        os.mkdir(artifact_path)

    # copy the binary to the path
    copy_path = os.path.join(artifact_path, filename)
    try:
        shutil.copy(filepath, copy_path)
    except shutil.SameFileError:
        pass # the macOS release copy fails with being the same file (already in the artifacts directory)

def main():
    """ Attempts to upload Asset to Github Release. Creates Release if it doesn't exist """
    try:
        args = parse_args()
        client = Github(args.api_key)
        repo = client.get_repo(CLOUDFLARED_REPO)
        release = get_or_create_release(repo, args.release_version, args.dry_run)

        if args.dry_run:
            logging.info("Skipping asset upload because of dry-run")
            return

        if os.path.isdir(args.path):
            onlyfiles = [f for f in listdir(args.path) if isfile(join(args.path, f))]
            for filename in onlyfiles:
                binary_path = os.path.join(args.path, filename)
                upload_asset(release, binary_path, filename, args.release_version, args.kv_account_id, args.namespace_id,
                args.kv_api_token)
        else:
            upload_asset(release, args.path, args.name, args.release_version, args.kv_account_id, args.namespace_id,
                args.kv_api_token)

    except Exception as e:
        logging.exception(e)
        exit(1)

main()
