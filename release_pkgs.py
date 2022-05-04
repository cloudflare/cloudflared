"""
    This is a utility for creating deb and rpm packages, signing them 
    and uploading them to a storage and adding metadata to workers KV.

    It has two over-arching responsiblities:
    1. Create deb and yum repositories from .deb and .rpm files. 
       This is also responsible for signing the packages and generally preparing 
       them to be in an uploadable state.
    2. Upload these packages to a storage and add metadata cross reference 
       for these to be accessed.
"""
import requests
import subprocess
import os
import io
import shutil
import logging
from hashlib import sha256

import boto3
from botocore.client import Config
from botocore.exceptions import ClientError

BASE_KV_URL = 'https://api.cloudflare.com/client/v4/accounts/'
# The front facing R2 URL to access assets from.
R2_ASSET_URL = 'https://demo-r2-worker.cloudflare-tunnel.workers.dev/'

class PkgUploader:
    def __init__(self, kv_api_token, namespace, account_id, bucket_name, client_id, client_secret):
        self.kv_api_token = kv_api_token
        self.namespace = namespace
        self.account_id = account_id
        self.bucket_name = bucket_name
        self.client_id = client_id
        self.client_secret = client_secret

    def send_to_kv(self, key, value):
        headers = {
            "Content-Type": "application/json",
            "Authorization": "Bearer " + self.kv_api_token,
        }

        kv_url = f"{BASE_KV_URL}{self.account_id}/storage/kv/namespaces/{self.namespace}/values/{key}"
        response = requests.put(
                kv_url,
                headers=headers,
                data=value
        )
    
        if response.status_code != 200:
            jsonResponse = response.json()
            errors = jsonResponse["errors"]
            if len(errors) > 0:
                raise Exception("failed to send to workers kv: {0}", errors[0])
            else:
                raise Exception("recieved error code: {0}", response.status_code)
    
    
    def send_pkg_info(self, binary, flavor, asset_name, arch, uploaded_package_location):
        key = f"pkg_{binary}_{flavor}_{arch}_{asset_name}"
        print(f"writing key:{key} , value: {uploaded_package_location}")
        self.send_to_kv(key, uploaded_package_location)
    
    
    def upload_pkg_to_r2(self, filename, upload_file_path):
        endpoint_url = f"https://{self.account_id}.r2.cloudflarestorage.com"
        token_secret_hash = sha256(self.client_secret.encode()).hexdigest()
         
        config = Config(
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
        flavor - flavor you want this to be distributed for (List of Strings)
        components - could be a channel like main/stable/beta
        archs - Architecture (List of Strings)
        description - (String)
        gpg_key_id - gpg key id of what you want to use to sign the packages.(String) 
    """
    def create_distribution_conf(self, 
            file_path,
            origin,
            label,
            flavors,
            archs,
            components, 
            description,
            gpg_key_id ):
        with open(file_path, "w") as distributions_file:
            for flavor in flavors:
                distributions_file.write(f"Origin: {origin}\n")
                distributions_file.write(f"Label: {label}\n")
                distributions_file.write(f"Codename: {flavor}\n")
                archs_list = " ".join(archs)
                distributions_file.write(f"Architectures: {archs_list}\n")
                distributions_file.write(f"Components: {components}\n")
                distributions_file.write(f"Description: {description} - {flavor}\n")
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
    def create_deb_pkgs(self, flavor, deb_file):
        self._clean_build_resources()
        subprocess.call(("reprepro", "includedeb", flavor, deb_file))

    """
        This is mostly useful to clear previously built db, dist and pool resources.
    """
    def _clean_build_resources(self):
        subprocess.call(("reprepro", "clearvanished"))

def upload_from_directories(pkg_uploader, directory, arch, release):
     for root, _ , files in os.walk(directory):
        for file in files:
            root_elements = root.split("/")
            upload_file_name = os.path.join(root, arch, release, file)
            flavor_prefix = root_elements[1]
            if root_elements[0] == "pool":
                upload_file_name = os.path.join(root, file)
                flavor_prefix = "deb"
            filename = os.path.join(root,file)
            try: 
                pkg_uploader.upload_pkg_to_r2(filename, upload_file_name)
            except ClientError as e:
                logging.error(e)
                return 

            # save to workers kv in the following formats
            # Example:
            # key : pkg_cloudflared_bullseye_InRelease,
            # value: https://r2.cloudflarestorage.com/dists/bullseye/amd64/2022_3_4/InRelease
            r2_asset_url = f"{R2_ASSET_URL}{upload_file_name}"
            pkg_uploader.send_pkg_info("cloudflared", flavor_prefix,  upload_file_name, arch, r2_asset_url)

            # TODO https://jira.cfops.it/browse/TUN-6163: Add a key for latest version.

""" 
    1. looks into a built_artifacts folder for cloudflared debs
    2. creates Packages.gz, InRelease (signed) files
    3. uploads them to Cloudflare R2 and
    4. adds a Workers KV reference

    pkg_creator, pkg_uploader: are instantiations of the two classes above.

    gpg_key_id: is an id indicating the key the package should be signed with. The public key of this id will be 
    uploaded to R2 so it can be presented to apt downloaders.

    release_version: is the cloudflared release version.
"""
def create_deb_packaging(pkg_creator, pkg_uploader, flavors, gpg_key_id, binary_name, arch, package_component, release_version):
    # set configuration for package creation.
    print(f"initialising configuration for {binary_name} , {arch}")
    pkg_creator.create_distribution_conf(
    "./conf/distributions",
    binary_name,
    binary_name,
    flavors,
    [arch],
    package_component,
    f"apt repository for {binary_name}",
    gpg_key_id)

    # create deb pkgs
    for flavor in flavors:
        print(f"creating deb pkgs for {flavor} and {arch}...")
        pkg_creator.create_deb_pkgs(flavor, f"./built_artifacts/cloudflared-linux-{arch}.deb")

    print("uploading to r2...")
    upload_from_directories(pkg_uploader, "dists", arch, release_version)
    upload_from_directories(pkg_uploader, "pool", arch, release_version)

    print("cleaning up directories...")
    shutil.rmtree("./dists")
    shutil.rmtree("./pool")
    shutil.rmtree("./db")

#TODO: https://jira.cfops.it/browse/TUN-6146 will extract this into it's own command line script.
if __name__ == "__main__":
    # initialise pkg creator
    pkg_creator = PkgCreator()
   
    # initialise pkg uploader
    bucket_name = os.getenv('R2_BUCKET_NAME')
    client_id = os.getenv('R2_CLIENT_ID')
    client_secret = os.getenv('R2_CLIENT_SECRET')
    tunnel_account_id = '5ab4e9dfbd435d24068829fda0077963'
    kv_namespace = os.getenv('KV_NAMESPACE')
    kv_api_token = os.getenv('KV_API_TOKEN')
    release_version = os.getenv('RELEASE_VERSION')
    gpg_key_id = os.getenv('GPG_KEY_ID')

    pkg_uploader = PkgUploader(kv_api_token, kv_namespace, tunnel_account_id, bucket_name, client_id, client_secret)

    archs = ["amd64", "386", "arm64"]
    flavors = ["bullseye", "buster", "bionic"]
    for arch in archs:
        create_deb_packaging(pkg_creator, pkg_uploader, flavors, gpg_key_id, "cloudflared", arch, "main", release_version)
