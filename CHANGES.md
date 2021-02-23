**Experimental**: This is a new format for release notes. The format and availability is subject to change.

## 2021.2.5

### New Features
- We introduce [Cloudflare One Routing](https://developers.cloudflare.com/cloudflare-one/tutorials/warp-to-tunnel) in beta mode, where users can create private networks with RFC 1918 IP addresses on Cloudflare's network. Clients running Cloudflare WARP client in the same organization can then connect to services made available by Argo Tunnel. Please share your feedback in the GitHub issue opened.


## 2021.2.4

### Bug Fixes
- Reverts the Improvement released in 2021.2.3 for CLI arguments as it introduced a regression where cloudflared failed to read URLs in configuration files.
- Cloudflared now logs the reason for failed connections if the error is recoverable.


## 2021.2.3
 
### Backward Incompatible Changes
- Removes db-connect. The Cloudflare Workers product will continue to support db-connect implementations with versions of cloudflared that predate this release and include support for db-connect.
 
### New Features
- Introduces support for proxy configurations with websockets in arbitrary TCP connections (#318).
 
### Improvements
- Nested command line arguments (such as `cloudflared tunnel run`) now consider CLI arguments even if they appear earlier in the command. E.g. `cloudflared --config config.yaml tunnel run` will now behave similarly to `cloudflared tunnel --config config.yaml run`
 
### Bug Fixes
- The maximum number of upstream connections is now limited by default which should fix reported issues of cloudflared exhausting CPU usage when faced with connectivity issues.

