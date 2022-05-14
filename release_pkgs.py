"""
    This is a utility for creating deb and rpm packages, signing them 
    and uploading them to a storage and adding metadata to workers KV.

    It has two over-arching responsiblities:
    1. Create deb and yum repositories from .deb and .rpm files. 
       This is also responsible for signing the packages and generally preparing 
       them to be in an uploadable state.
    2. Upload these packages to a storage in a format that apt and yum expect.
"""
import subprocess
import os
import argparse
import logging
import shutil
from hashlib import sha256

import boto3
from botocore.client import Config
from botocore.exceptions import ClientError

# The front facing R2 URL to access assets from.
R2_ASSET_URL = 'https://demo-r2-worker.cloudflare-tunnel.workers.dev/'

class PkgUploader:
    def __init__(self, account_id, bucket_name, client_id, client_secret):
        self.account_id = account_id
        self.bucket_name = bucket_name
        self.client_id = client_id
        self.client_secret = client_secret

    def upload_pkg_to_r2(self, filename, upload_file_path):
        endpoint_url = f"https://{self.account_id}.r2.cloudflarestorage.com"
        token_secret_hash = sha256(self.client_secret.encode()).hexdigest()
         
        config = Config(
            region_name = 'auto',
            s3={
                "addressing_style": "path",
            }
        )
         
        r2 = boto3.client(
            "s3",
            endpoint_url=endpoint_url,
            aws_access_key_id=self.client_id,
            aws_secret_access_key=token_secret_hash,
            config=config,
        )

        print(f"uploading asset: {filename} to {upload_file_path}...")
        try:
            r2.upload_file(filename, self.bucket_name, upload_file_path)
        except ClientError as e:
            raise e

                  
class PkgCreator:
    """
        The distribution conf is what dictates to reprepro, the debian packaging building
        and signing tool we use, what distros to support, what GPG key to use for signing
        and what to call the debian binary etc. This function creates it "./conf/distributions".

        origin - name of your package (String)
        label - label of your package (could be same as the name) (String)
        release - release you want this to be distributed for (List of Strings)
        components - could be a channel like main/stable/beta
        archs - Architecture (List of Strings)
        description - (String)
        gpg_key_id - gpg key id of what you want to use to sign the packages.(String) 
    """
    def create_distribution_conf(self, 
            file_path,
            origin,
            label,
            releases,
            archs,
            components, 
            description,
            gpg_key_id ):
        with open(file_path, "w") as distributions_file:
            for release in releases:
                distributions_file.write(f"Origin: {origin}\n")
                distributions_file.write(f"Label: {label}\n")
                distributions_file.write(f"Codename: {release}\n")
                archs_list = " ".join(archs)
                distributions_file.write(f"Architectures: {archs_list}\n")
                distributions_file.write(f"Components: {components}\n")
                distributions_file.write(f"Description: {description} - {release}\n")
                distributions_file.write(f"SignWith: {gpg_key_id}\n")
                distributions_file.write("\n")
        return distributions_file

    """
        Uses the reprepro tool to generate packages, sign them and create the InRelease as specified
        by the distribution_conf file. 

        This function creates three folders db, pool and dist. 
        db and pool contain information and metadata about builds. We can ignore these.
        dist: contains all the pkgs and signed releases that are necessary for an apt download.
    """
    def create_deb_pkgs(self, release, deb_file):
        self._clean_build_resources()
        subprocess.call(("reprepro", "includedeb", release, deb_file), timeout=120)

    """
        This is mostly useful to clear previously built db, dist and pool resources.
    """
    def _clean_build_resources(self):
        subprocess.call(("reprepro", "clearvanished"), timeout=120)

    # TODO https://jira.cfops.it/browse/TUN-6209 : Sign these packages.
    def create_rpm_pkgs(self, artifacts_path):
        self._setup_rpm_pkg_directories(artifacts_path)
        subprocess.call(("createrepo", "./rpm"), timeout=120)

    """
        sets up the RPM directories in the following format:
        - rpm 
           - aarch64
           - x86_64
           - 386

        this assumes the assets are in the format <prefix>-<aarch64/x86_64/386>.rpm
    """
    def _setup_rpm_pkg_directories(self, artifacts_path, archs=["aarch64", "x86_64", "386"]):
        for arch in archs:
            for root, _ , files in os.walk(artifacts_path):
                for file in files:
                    if file.endswith(f"{arch}.rpm"):
                        new_dir = f"./rpm/{arch}"
                        os.makedirs(new_dir, exist_ok=True)
                        old_path = os.path.join(root, file)
                        new_path = os.path.join(new_dir, file)
                        shutil.copyfile(old_path, new_path)

"""
    Walks through a directory and uploads it's assets to R2.
    directory : root directory to walk through (String).
    release: release string. If this value is none, a specific release path will not be created 
              and the release will be uploaded to the default path. 
    binary: name of the binary to upload
"""
def upload_from_directories(pkg_uploader, directory, release, binary):
     for root, _ , files in os.walk(directory):
        for file in files:
            upload_file_name = os.path.join(binary, root, file)
            if release:
                upload_file_name = os.path.join(release, upload_file_name)
            filename = os.path.join(root,file)
            try: 
                pkg_uploader.upload_pkg_to_r2(filename, upload_file_name)
            except ClientError as e:
                logging.error(e)
                return 

""" 
    1. looks into a built_artifacts folder for cloudflared debs
    2. creates Packages.gz, InRelease (signed) files
    3. uploads them to Cloudflare R2 

    pkg_creator, pkg_uploader: are instantiations of the two classes above.

    gpg_key_id: is an id indicating the key the package should be signed with. The public key of this id will be 
    uploaded to R2 so it can be presented to apt downloaders.

    release_version: is the cloudflared release version.
"""
def create_deb_packaging(pkg_creator, pkg_uploader, releases, gpg_key_id, binary_name, archs, package_component, release_version):
    # set configuration for package creation.
    print(f"initialising configuration for {binary_name} , {archs}")
    pkg_creator.create_distribution_conf(
    "./conf/distributions",
    binary_name,
    binary_name,
    releases,
    archs,
    package_component,
    f"apt repository for {binary_name}",
    gpg_key_id)

    # create deb pkgs
    for release in releases:
        for arch in archs:
            print(f"creating deb pkgs for {release} and {arch}...")
            pkg_creator.create_deb_pkgs(release, f"./built_artifacts/cloudflared-linux-{arch}.deb")

    print("uploading latest to r2...")
    upload_from_directories(pkg_uploader, "dists", None, binary_name)
    upload_from_directories(pkg_uploader, "pool", None, binary_name)

    print(f"uploading versioned release {release_version} to r2...")
    upload_from_directories(pkg_uploader, "dists", release_version, binary_name)
    upload_from_directories(pkg_uploader, "pool", release_version, binary_name)

def create_rpm_packaging(pkg_creator, pkg_uploader, artifacts_path, release_version, binary_name):
    print(f"creating rpm pkgs...")
    pkg_creator.create_rpm_pkgs(artifacts_path)

    print("uploading latest to r2...")
    upload_from_directories(pkg_uploader, "rpm", None, binary_name)

    print(f"uploading versioned release {release_version} to r2...")
    upload_from_directories(pkg_uploader, "rpm", release_version, binary_name)


def parse_args():
    parser = argparse.ArgumentParser(
        description="Creates linux releases and uploads them in a packaged format"
    )

    parser.add_argument(
            "--bucket", default=os.environ.get("R2_BUCKET_NAME"), help="R2 Bucket name"
    )
    parser.add_argument(
            "--id", default=os.environ.get("R2_CLIENT_ID"), help="R2 Client ID"
    )
    parser.add_argument(
            "--secret", default=os.environ.get("R2_CLIENT_SECRET"), help="R2 Client Secret"
    )
    parser.add_argument(
            "--account", default=os.environ.get("R2_ACCOUNT_ID"), help="R2 Account Tag"
    )
    parser.add_argument(
            "--release-tag", default=os.environ.get("RELEASE_VERSION"), help="Release version you want your pkgs to be\
            prefixed with"
    )

    parser.add_argument(
           "--binary", default=os.environ.get("BINARY_NAME"), help="The name of the binary the packages are for"
    )

    parser.add_argument(
            "--gpg-key-id", default=os.environ.get("GPG_KEY_ID"), help="gpg key ID that's being used to sign release\
            packages."
    )

    parser.add_argument(
            "--deb-based-releases", default=["bookworm", "bullseye", "buster", "jammy", "impish", "focal", "bionic"],
            help="list of debian based releases that need to be packaged for"
    )

    parser.add_argument(
            "--archs", default=["amd64", "386", "arm64"], help="list of architectures we want to package for. Note that\
            it is the caller's responsiblity to ensure that these debs are already present in a directory. This script\
            will not build binaries or create their debs."
            )
    args = parser.parse_args()

    return args

if __name__ == "__main__":
    try:
        args = parse_args()
    except Exception as e:
        logging.exception(e)
        exit(1)

    pkg_creator = PkgCreator()
    pkg_uploader = PkgUploader(args.account, args.bucket, args.id, args.secret)
    create_deb_packaging(pkg_creator, pkg_uploader, args.deb_based_releases, args.gpg_key_id, args.binary, 
            args.archs, "main", args.release_tag)
    
    create_rpm_packaging(pkg_creator, pkg_uploader, "./built_artifacts", args.release_tag, args.binary )
