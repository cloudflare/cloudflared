# Changelog

## v1.4.0 (2021-02-01)

- Support colons in cgroup names.
- Remove linters from runtime dependencies.

## v1.3.0 (2020-01-23)

- Migrate to Go modules.

## v1.2.0 (2018-02-22)

- Fixed quota clamping to always round down rather than up; Rather than
  guaranteeing constant throttling at saturation, instead assume that the
  fractional CPU was added as a hedge for factors outside of Go's scheduler.

## v1.1.0 (2017-11-10)

- Log the new value of `GOMAXPROCS` rather than the current value.
- Make logs more explicit about whether `GOMAXPROCS` was modified or not.
- Allow customization of the minimum `GOMAXPROCS`, and modify default from 2 to 1.

## v1.0.0 (2017-08-09)

- Initial release.
