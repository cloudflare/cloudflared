#!/bin/bash
set -e -o pipefail

# Fetch cloudflared from the artifacts folder
mv ./artifacts/cloudflared ./cloudflared

python3 -m venv env
. env/bin/activate

pip install --upgrade -r component-tests/requirements.txt

# Creates and routes a Named Tunnel for this build. Also constructs
# config file from env vars.
python3 component-tests/setup.py --type create

# Define the cleanup function
cleanup() {
    # The Named Tunnel is deleted and its route unprovisioned here.
    python3 component-tests/setup.py --type cleanup
}

# The trap will call the cleanup function on script exit
trap cleanup EXIT

pytest component-tests -o log_cli=true --log-cli-level=INFO --junit-xml=report.xml
