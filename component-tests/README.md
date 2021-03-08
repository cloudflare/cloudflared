# Requirements
1. Python 3.7 or later with packages in the given `requirements.txt`
   - E.g. with conda:
   - `conda create -n component-tests python=3.7`  
   - `conda activate component-tests`
   - `pip3 install -r requirements.txt`

2. Create a config yaml file, for example:
```
cloudflared_binary: "cloudflared"
tunnel: "3d539f97-cd3a-4d8e-c33b-65e9099c7a8d"
credentials_file: "/Users/tunnel/.cloudflared/3d539f97-cd3a-4d8e-c33b-65e9099c7a8d.json"
classic_hostname: "classic-tunnel-component-tests.example.com"
origincert: "/Users/tunnel/.cloudflared/cert.pem"
```

# How to run
Specify path to config file via env var `COMPONENT_TESTS_CONFIG`. This is required.
## All tests
Run `pytest` inside this(component-tests) folder

## Specific files
Run `pytest <file 1 name>.py <file 2 name>.py`

## Specific tests
Run `pytest file.py -k <test 1 name> -k <test 2 name>`

## Live Logging
Running with `-o log_cli=true` outputs logging to CLI as the tests are. By default the log level is WARN.
`--log-cli-level` control logging level.
For example, to log at info level, run `pytest -o log_cli=true --log-cli-level=INFO`.
See https://docs.pytest.org/en/latest/logging.html#live-logs for more documentation on logging.