"""
    This is a utility for creating deb and rpm packages, signing them 
    and uploading them to a storage and adding metadata to workers KV.

    It has two over-arching responsiblities:
    1. Create deb and yum repositories from .deb and .rpm files. 
       This is also responsible for signing the packages and generally preparing 
       them to be in an uploadable state.
    2. Upload these packages to a storage in a format that apt and yum expect.
"""
import argparse
import base64
import logging
import os
import shutil
from hashlib import sha256
from pathlib import Path
from subprocess import Popen, PIPE

import boto3
import gnupg
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
            region_name='auto',
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

        print(f"uploading asset: {filename} to {upload_file_path} in bucket{self.bucket_name}...")
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
                                 gpg_key_id):
        with open(file_path, "w+") as distributions_file:
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
        print(f"creating deb pkgs: {release} : {deb_file}")
        p = Popen(["reprepro", "includedeb", release, deb_file], stdout=PIPE, stderr=PIPE)
        out, err = p.communicate()
        if p.returncode != 0:
            print(f"create deb_pkgs result => {out}, {err}")
            raise

    def create_rpm_pkgs(self, artifacts_path, gpg_key_name):
        self._setup_rpm_pkg_directories(artifacts_path, gpg_key_name)
        p = Popen(["createrepo", "./rpm"], stdout=PIPE, stderr=PIPE)
        out, err = p.communicate()
        if p.returncode != 0:
            print(f"create rpm_pkgs result => {out}, {err}")
            raise

        self._sign_repomd()

    """
        creates a <binary>.repo file with details like so
        [cloudflared-stable]
        name=cloudflared-stable
        baseurl=https://pkg.cloudflare.com/cloudflared/rpm
        enabled=1
        type=rpm
        gpgcheck=1
        gpgkey=https://pkg.cloudflare.com/cloudflare-main.gpg
    """

    def create_repo_file(self, file_path, binary_name, baseurl, gpgkey_url):
        with open(os.path.join(file_path, binary_name + '.repo'), "w+") as repo_file:
            repo_file.write(f"[{binary_name}-stable]")
            repo_file.write(f"{binary_name}-stable")
            repo_file.write(f"baseurl={baseurl}/rpm")
            repo_file.write("enabled=1")
            repo_file.write("type=rpm")
            repo_file.write("gpgcheck=1")
            repo_file.write(f"gpgkey={gpgkey_url}")

    def _sign_rpms(self, file_path):
        p = Popen(["rpm", "--define", f"_gpg_name {gpg_key_name}", "--addsign", file_path], stdout=PIPE, stderr=PIPE)
        out, err = p.communicate()
        if p.returncode != 0:
            print(f"rpm sign result result => {out}, {err}")
            raise

    def _sign_repomd(self):
        p = Popen(["gpg", "--batch", "--detach-sign", "--armor", "./rpm/repodata/repomd.xml"], stdout=PIPE, stderr=PIPE)
        out, err = p.communicate()
        if p.returncode != 0:
            print(f"sign repomd result => {out}, {err}")
            raise

    """
        sets up and signs the RPM directories in the following format:
        - rpm 
           - aarch64
           - x86_64
           - 386

        this assumes the assets are in the format <prefix>-<aarch64/x86_64/386>.rpm
    """

    def _setup_rpm_pkg_directories(self, artifacts_path, gpg_key_name, archs=["aarch64", "x86_64", "386"]):
        for arch in archs:
            for root, _, files in os.walk(artifacts_path):
                for file in files:
                    if file.endswith(f"{arch}.rpm"):
                        new_dir = f"./rpm/{arch}"
                        os.makedirs(new_dir, exist_ok=True)
                        old_path = os.path.join(root, file)
                        new_path = os.path.join(new_dir, file)
                        shutil.copyfile(old_path, new_path)
                        self._sign_rpms(new_path)

    """
        imports gpg keys into the system so reprepro and createrepo can use it to sign packages.
        it returns the GPG ID after a successful import
    """

    def import_gpg_keys(self, private_key, public_key):
        gpg = gnupg.GPG()
        private_key = base64.b64decode(private_key)
        gpg.import_keys(private_key)
        public_key = base64.b64decode(public_key)
        gpg.import_keys(public_key)
        data = gpg.list_keys(secret=True)
        return (data[0]["fingerprint"], data[0]["uids"][0])

    """
        basically rpm --import <key_file>
        This enables us to sign rpms.
    """

    def import_rpm_key(self, public_key):
        file_name = "pb.key"
        with open(file_name, "wb") as f:
            public_key = base64.b64decode(public_key)
            f.write(public_key)

        p = Popen(["rpm", "--import", file_name], stdout=PIPE, stderr=PIPE)
        out, err = p.communicate()
        if p.returncode != 0:
            print(f"create rpm import result => {out}, {err}")
            raise


"""
    Walks through a directory and uploads it's assets to R2.
    directory : root directory to walk through (String).
    release: release string. If this value is none, a specific release path will not be created 
              and the release will be uploaded to the default path. 
    binary: name of the binary to upload
"""


def upload_from_directories(pkg_uploader, directory, release, binary):
    for root, _, files in os.walk(directory):
        for file in files:
            upload_file_name = os.path.join(binary, root, file)
            if release:
                upload_file_name = os.path.join(release, upload_file_name)
            filename = os.path.join(root, file)
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

    release_version: is the cloudflared release version. Only provide this if you want a permanent backup.
"""


def create_deb_packaging(pkg_creator, pkg_uploader, releases, gpg_key_id, binary_name, archs, package_component,
                         release_version):
    # set configuration for package creation.
    print(f"initialising configuration for {binary_name} , {archs}")
    Path("./conf").mkdir(parents=True, exist_ok=True)
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

    if release_version:
        print(f"uploading versioned release {release_version} to r2...")
        upload_from_directories(pkg_uploader, "dists", release_version, binary_name)
        upload_from_directories(pkg_uploader, "pool", release_version, binary_name)


def create_rpm_packaging(
        pkg_creator,
        pkg_uploader,
        artifacts_path,
        release_version,
        binary_name,
        gpg_key_name,
        base_url,
        gpg_key_url,
):
    print(f"creating rpm pkgs...")
    pkg_creator.create_rpm_pkgs(artifacts_path, gpg_key_name)
    pkg_creator.create_repo_file(artifacts_path, binary_name, base_url, gpg_key_url)

    print("uploading latest to r2...")
    upload_from_directories(pkg_uploader, "rpm", None, binary_name)

    if release_version:
        print(f"uploading versioned release {release_version} to r2...")
        upload_from_directories(pkg_uploader, "rpm", release_version, binary_name)


def parse_args():
    parser = argparse.ArgumentParser(
        description="Creates linux releases and uploads them in a packaged format"
    )

    parser.add_argument(
        "--bucket", default=os.environ.get("R2_BUCKET"), help="R2 Bucket name"
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
            prefixed with. Leave empty if you don't want tagged release versions backed up to R2."
    )

    parser.add_argument(
        "--binary", default=os.environ.get("BINARY_NAME"), help="The name of the binary the packages are for"
    )

    parser.add_argument(
        "--gpg-private-key", default=os.environ.get("LINUX_SIGNING_PRIVATE_KEY"), help="GPG private key to sign the\
            packages"
    )

    parser.add_argument(
        "--gpg-public-key", default=os.environ.get("LINUX_SIGNING_PUBLIC_KEY"), help="GPG public key used for\
            signing packages"
    )

    parser.add_argument(
        "--gpg-public-key-url", default=os.environ.get("GPG_PUBLIC_KEY_URL"), help="GPG public key url that\
            downloaders can use to verify signing"
    )

    parser.add_argument(
        "--pkg-upload-url", default=os.environ.get("PKG_URL"), help="URL to be used by downloaders"
    )

    parser.add_argument(
        "--deb-based-releases", default=["bookworm", "bullseye", "buster", "jammy", "impish", "focal", "bionic",
                                         "xenial", "trusty"],
        help="list of debian based releases that need to be packaged for"
    )

    parser.add_argument(
        "--archs", default=["amd64", "386", "arm64", "arm", "armhf"], help="list of architectures we want to package for. Note that\
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
    (gpg_key_id, gpg_key_name) = pkg_creator.import_gpg_keys(args.gpg_private_key, args.gpg_public_key)
    pkg_creator.import_rpm_key(args.gpg_public_key)

    pkg_uploader = PkgUploader(args.account, args.bucket, args.id, args.secret)
    print(f"signing with gpg_key: {gpg_key_id}")
    create_deb_packaging(pkg_creator, pkg_uploader, args.deb_based_releases, gpg_key_id, args.binary, args.archs,
                         "main", args.release_tag)

    create_rpm_packaging(
        pkg_creator,
        pkg_uploader,
        "./built_artifacts",
        args.release_tag,
        args.binary,
        gpg_key_name,
        args.gpg_public_key_url,
        args.pkg_upload_url,
    )
