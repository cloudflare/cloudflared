# Experimental: This is a new format for release notes. The format and availability is subject to change.

## 2021.2.3
 
### Backward Incompatible Changes
[db-connect] Removes db-connect from `cloudflared`. The Cloudflare Workers product will continue to support db-connect implementations with versions of `cloudflared` that predate this release and include support for db-connect.
 
### New Features
[TCP connects] Introduces support for proxy configurations with websockets in arbitrary TCP connections (#318)
 
### Improvements
[command line interface] Nested commands (such as cloudflared tunnel run) now consider CLI arguments even if they appear earlier in the command. E.g. `cloudflared --config config.yaml tunnel run` will now behave similarly to `cloudflared tunnel --config config.yaml run`
 
### Bug Fixes
[dns-proxy] The maximum number of upstream connections is now limited by default which should fix reported issues of cloudflared exhausting CPU usage when faced with connectivity issues.

