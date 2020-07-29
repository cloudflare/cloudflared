#!/usr/bin/python3
"""
Creates Github Releases and uploads assets
"""

import argparse
import logging
import os
import shutil

from github import Github, GithubException, UnknownObjectException

FORMAT = "%(levelname)s - %(asctime)s: %(message)s"
logging.basicConfig(format=FORMAT)

CLOUDFLARED_REPO = os.environ.get("GITHUB_REPO", "cloudflare/cloudflared")
GITHUB_CONFLICT_CODE = "already_exists"

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

    # We dont want to create a new release tag if one doesnt already exist
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

    if not args.name:
        logging.error("Missing asset name")
        is_valid = False

    if not args.api_key:
        logging.error("Missing API key")
        is_valid = False

    if is_valid:
        return args

    parser.print_usage()
    exit(1)


def main():
    """ Attempts to upload Asset to Github Release. Creates Release if it doesnt exist """
    try:
        args = parse_args()
        client = Github(args.api_key)
        repo = client.get_repo(CLOUDFLARED_REPO)
        release = get_or_create_release(repo, args.release_version, args.dry_run)

        if args.dry_run:
            logging.info("Skipping asset upload because of dry-run")
            return

        release.upload_asset(args.path, name=args.name)

        # create the artifacts directory if it doesn't exist 
        artifact_path = os.path.join(os.getcwd(), 'artifacts') 
        if not os.path.isdir(artifact_path):
            os.mkdir(artifact_path)

        # copy the binary to the path
        copy_path = os.path.join(artifact_path, args.name)
        shutil.copy(args.path, copy_path)



    except Exception as e:
        logging.exception(e)
        exit(1)


main()
