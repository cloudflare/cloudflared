# Requirements
1. Python 3.7 or later with packages in the given `requirements.txt`
   - E.g. with conda:
   - `conda create -n component-tests python=3.7`  
   - `conda activate component-tests`
   - `pip3 install -r requirements.txt`

# How to run
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