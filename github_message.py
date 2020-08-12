#!/usr/bin/python3
"""
Creates Github Releases Notes with content hashes
"""

import argparse
import logging
import os
import hashlib
import glob

from github import Github, GithubException, UnknownObjectException

FORMAT = "%(levelname)s - %(asctime)s: %(message)s"
logging.basicConfig(format=FORMAT)

CLOUDFLARED_REPO = os.environ.get("GITHUB_REPO", "cloudflare/cloudflared")
GITHUB_CONFLICT_CODE = "already_exists"

def get_sha256(filename):
    """ get the sha256 of a file """
    sha256_hash = hashlib.sha256()
    with open(filename,"rb") as f:
        for byte_block in iter(lambda: f.read(4096),b""):
            sha256_hash.update(byte_block)
        return sha256_hash.hexdigest()


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

        msg = release.body

        for filename in glob.glob("artifacts/*.*"):
            pkg_hash = get_sha256(filename)
            # add the sha256 of the new artifact to the release message body
            msg = update_or_add_message(msg, filename, pkg_hash)

        if args.dry_run:
            logging.info("Skipping asset upload because of dry-run")
            return

        # update the release body text
        release.update_release(args.release_version, msg)

    except Exception as e:
        logging.exception(e)
        exit(1)


main()