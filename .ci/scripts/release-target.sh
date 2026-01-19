#!/bin/bash
set -e -o pipefail

# Check if a make target is provided as an argument
if [ $# -eq 0 ]; then
    echo "Error: Make target argument is required"
    echo "Usage: $0 <make-target>"
    exit 1
fi

MAKE_TARGET=$1

python3 -m venv venv
source venv/bin/activate

# Our release scripts are written in python, so we should install their dependecies here.
pip install pynacl==1.4.0 pygithub==1.55 boto3==1.42.30 python-gnupg==0.4.9
make $MAKE_TARGET
