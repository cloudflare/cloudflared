# Requirements
1. Python 3.10 or later with packages in the given `requirements.txt`
   - E.g. with venv:
   - `python3 -m venv ./.venv`  
   - `source ./.venv/bin/activate`
   - `python3 -m pip install -r requirements.txt`

2. Create a config yaml file, for example:
```
cloudflared_binary: "cloudflared"
tunnel: "3d539f97-cd3a-4d8e-c33b-65e9099c7a8d"
credentials_file: "/Users/tunnel/.cloudflared/3d539f97-cd3a-4d8e-c33b-65e9099c7a8d.json"
origincert: "/Users/tunnel/.cloudflared/cert.pem"
ingress:
- hostname: named-tunnel-component-tests.example.com
  service: hello_world
- service: http_status:404
```

3. Route hostname to the tunnel. For the example config above, we can do that via
```
   cloudflared tunnel route dns 3d539f97-cd3a-4d8e-c33b-65e9099c7a8d named-tunnel-component-tests.example.com
```

4. Turn on linter
If you are using Visual Studio, follow https://code.visualstudio.com/docs/python/linting to turn on linter.

5. Turn on formatter
If you are using Visual Studio, follow https://code.visualstudio.com/docs/python/editing#_formatting
to turn on formatter and https://marketplace.visualstudio.com/items?itemName=cbrevik.toggle-format-on-save
to turn on format on save.

6. If you have cloudflared running as a service on your machine, you can either stop the service or ignore the service tests
via `--ignore test_service.py`

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