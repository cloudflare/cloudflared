## 2022.7.1
### New Features
- It is now possible to connect cloudflared tunnel to Cloudflare Global Network with IPv6. See `cloudflared tunnel --help` and look for `edge-ip-version` for more information. For now, the default behavior is to still connect with IPv4 only.

### Bug Fixes
- Several bug fixes related with QUIC transport (used between cloudflared tunnel and Cloudflare Global Network). Updating to this version is highly recommended.

## 2022.4.0
### Bug Fixes
- `cloudflared tunnel run` no longer logs the Tunnel token or JSON credentials in clear text as those are the secret
that allows to run the Tunnel.

## 2022.3.4
### New Features
- It is now possible to retrieve the credentials that allow to run a Tunnel in case you forgot/lost them. This is
achievable with: `cloudflared tunnel token --cred-file /path/to/file.json TUNNEL`. This new feature only works for
Tunnels created with cloudflared version 2022.3.0 or more recent.

### Bug Fixes
- `cloudflared service install` now starts the underlying agent service on Linux operating system (similarly to the
behaviour in Windows and MacOS).

## 2022.3.3
### Bug Fixes
- `cloudflared service install` now starts the underlying agent service on Windows operating system (similarly to the
behaviour in MacOS).

## 2022.3.1
### Bug Fixes
- Various fixes to the reliability of `quic` protocol, including an edge case that could lead to cloudflared crashing.

## 2022.3.0
### New Features
- It is now possible to configure Ingress Rules to point to an origin served by unix socket with either HTTP or HTTPS.
If the origin starts with `unix:/` then we assume HTTP (existing behavior). Otherwise, the origin can start with
`unix+tls:/` for HTTPS.

## 2022.2.1
### New Features
- This project now has a new LICENSE that is more compliant with open source purposes.

### Bug Fixes
- Various fixes to the reliability of `quic` protocol.

## 2022.1.3
### New Features
- New `cloudflared tunnel vnet` commands to allow for private routing to be virtualized. This means that the same CIDR
can now be used to point to two different Tunnels with `cloudflared tunnel route ip` command. More information will be
made available on blog.cloudflare.com and developers.cloudflare.com/cloudflare-one once the feature is globally available.

### Bug Fixes
- Correctly handle proxying UDP datagrams with no payload.
- Bug fix for origins that use Server-Sent Events (SSE).

## 2022.1.0
### Improvements
- If a specific `protocol` property is defined (e.g. for `quic`), cloudflared no longer falls back to an older protocol
(such as `http2`) in face of connectivity errors. This is important because some features are only supported in a specific
protocol (e.g. UDP proxying only works for `quic`). Hence, if a user chooses a protocol, cloudflared now adheres to it
no matter what.

### Bug Fixes
- Stopping cloudflared running with `quic` protocol now respects graceful shutdown.

## 2021.12.2
### Bug Fixes
- Fix logging when `quic` transport is used and UDP traffic is proxied.
- FIPS compliant cloudflared binaries will now be released as separate artifacts. Recall that these are only for linux
and amd64.

## 2021.12.1
### Bug Fixes
 - Fixes Github issue #530 where cloudflared 2021.12.0 could not reach origins that were HTTPS and using certain encryption
methods forbidden by FIPS compliance (such as Let's Encrypt certificates). To address this fix we have temporarily reverted
FIPS compliance from amd64 linux binaries that was recently introduced (or fixed actually as it was never working before).

## 2021.12.0
### New Features
- Cloudflared binary released for amd64 linux is now FIPS compliant.

### Improvements
- Logging about connectivity to Cloudflare edge now only yields `ERR` level logging if there are no connections to
Cloudflare edge that are active. Otherwise it logs `WARN` level.
 
### Bug Fixes
- Fixes Github issue #501.

## 2021.11.0
### Improvements
- Fallback from `protocol:quic` to `protocol:http2` immediately if UDP connectivity isn't available. This could be because of a firewall or 
egress rule.

## 2021.10.4
### Improvements
- Collect quic transport metrics on RTT, packets and bytes transferred.

### Bug Fixes
- Fix race condition that was writing to the connection after the http2 handler returns.

## 2021.9.2

### New features
- `cloudflared` can now run with `quic` as the underlying tunnel transport protocol. To try it, change or add "protocol: quic" to your config.yml file or
run cloudflared with the `--protocol quic` flag. e.g:
    `cloudflared tunnel --protocol quic run <tunnel-name>`

### Bug Fixes
- Fixed some generic transport bugs in `quic` mode. It's advised to upgrade to at least this version (2021.9.2) when running `cloudflared`
with `quic` protocol.
- `cloudflared` docker images will now show version.


## 2021.8.4
### Improvements
- Temporary tunnels (those hosted on trycloudflare.com that do not require a Cloudflare login) now run as Named Tunnels
underneath. We recall that these tunnels should not be relied upon for production usage as they come with no guarantee
of uptime. Previous cloudflared versions will soon be unable to run legacy temporary tunnels and will require an update
(to this version or more recent).

## 2021.8.2
### Improvements
- Because Equinox os shutting down, all cloudflared releases are now present [here](https://github.com/cloudflare/cloudflared/releases).
[Equinox](https://dl.equinox.io/cloudflare/cloudflared/stable) will no longer receive updates. 

## 2021.8.0
### Bug fixes
- Prevents tunnel from accidentally running when only proxy-dns should run. 

### Improvements
- If auto protocol transport lookup fails, we now default to a transport instead of not connecting.

## 2021.6.0
### Bug Fixes
- Fixes a http2 transport (the new default for Named Tunnels) to work with unix socket origins.


## 2021.5.10
### Bug Fixes
- Fixes a memory leak in h2mux transport that connects cloudflared to Cloudflare edge.


## 2021.5.9
### New Features
- Uses new Worker based login helper service to facilitate token exchange in cloudflared flows.

### Bug Fixes
- Fixes Centos-7 builds.

## 2021.5.8
### New Features
- When creating a DNS record to point a hostname at a tunnel, you can now use --overwrite-dns to overwrite any existing
  DNS records with that hostname. This works both when using the CLI to provision DNS, as well as when starting an adhoc
  named tunnel, e.g.:
  - `cloudflared tunnel route dns --overwrite-dns foo-tunnel foo.example.com`
  - `cloudflared tunnel --overwrite-dns --name foo-tunnel --hostname foo.example.com`

## 2021.5.7
### New Features
- Named Tunnels will automatically select the protocol to connect to Cloudflare's edge network.

## 2021.5.0

### New Features
- It is now possible to run the same tunnel using more than one `cloudflared` instance. This is a server-side change and
  is compatible with any client version that uses Named Tunnels.

  To get started, visit our [developer documentation](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/run-tunnel/deploy-cloudflared-replicas).
- `cloudflared tunnel ingress validate` will now warn about unused keys in your config file. This is helpful for
  detecting typos in your config.
- If `cloudflared` detects it is running inside a Linux container, it will limit itself to use only the number of CPUs
  the pod has been granted, instead of trying to use every CPU available.

## 2021.4.0

### Bug Fixes

- Fixed proxying of websocket requests to avoid possibility of losing initial frames that were sent in the same TCP
  packet as response headers [#345](https://github.com/cloudflare/cloudflared/issues/345).
- `proxy-dns` option now works in conjunction with running a named tunnel [#346](https://github.com/cloudflare/cloudflared/issues/346).

## 2021.3.6

### Bug Fixes

- Reverted 2021.3.5 improvement to use HTTP/2 in a best-effort manner between cloudflared and origin services because
  it was found to break in some cases.

## 2021.3.5

### Improvements

 - HTTP/2 transport is now always chosen if origin server supports it and the service url scheme is HTTPS.
   This was previously done in a best attempt manner.

### Bug Fixes

 - The MacOS binaries were not successfully released in 2021.3.3 and 2021.3.4. This release is aimed at addressing that.

## 2021.3.3

### Improvements

- Tunnel create command, as well as, running ad-hoc tunnels using `cloudflared tunnel -name NAME`, will not overwrite
  existing files when writing tunnel credentials.

### Bug Fixes

- Tunnel create and delete commands no longer use path to credentials from the configuration file.
  If you need ot place tunnel credentials file at a specific location, you must use `--credentials-file` flag.
- Access ssh-gen creates properly named keys for SSH short lived certs.


## 2021.3.2

### New Features

- It is now possible to obtain more detailed information about the cloudflared connectors to Cloudflare Edge via
  `cloudflared tunnel info <name/uuid>`. It is possible to sort the output as well as output in different formats,
  such as: `cloudflared tunnel info --sort-by version --invert-sort --output json <name/uuid>`.
  You can obtain more information via `cloudflared tunnel info --help`.

### Bug Fixes

- Don't look for configuration file in default paths when `--config FILE` flag is present after `tunnel` subcommand.
- cloudflared access token command now functions correctly with the new token-per-app change from 2021.3.0.


## 2021.3.0

### New Features

- [Cloudflare One Routing](https://developers.cloudflare.com/cloudflare-one/tutorials/warp-to-tunnel) specific commands
  now show up in the `cloudflared tunnel route --help` output.
- There is a new ingress type that allows cloudflared to proxy SOCKS5 as a bastion. You can use it with an ingress
  rule by adding `service: socks-proxy`. Traffic is routed to any destination specified by the SOCKS5 packet but only
  if allowed by a rule. In the following example we allow proxying to a certain CIDR but explicitly forbid one address
  within it:
```
ingress:
  - hostname: socks.example.com
    service: socks-proxy
    originRequest:
      ipRules:
        - prefix: 192.168.1.8/32
          allow: false
        - prefix: 192.168.1.0/24
          ports: [80, 443]
          allow: true
```


### Improvements

- Nested commands, such as `cloudflared tunnel run`, now consider CLI arguments even if they appear earlier on the
  command. For instance, `cloudflared --config config.yaml tunnel run` will now behave the same as
  `cloudflared tunnel --config config.yaml run`
- Warnings are now shown in the output logs whenever cloudflared is running without the most recent version and
  `no-autoupdate` is `true`.
- Access tokens are now stored per Access App instead of per request path. This decreases the number of times that the
  user is required to authenticate with an Access policy redundantly.

### Bug Fixes

- GitHub [PR #317](https://github.com/cloudflare/cloudflared/issues/317) was broken in 2021.2.5 and is now fixed again.

## 2021.2.5

### New Features

- We introduce [Cloudflare One Routing](https://developers.cloudflare.com/cloudflare-one/tutorials/warp-to-tunnel) in
  beta mode. Cloudflare customer can now connect users and private networks with RFC 1918 IP addresses via the
  Cloudflare edge network. Users running Cloudflare WARP client in the same organization can connect to the services
  made available by Argo Tunnel IP routes. Please share your feedback in the GitHub issue tracker.

## 2021.2.4

### Bug Fixes

- Reverts the Improvement released in 2021.2.3 for CLI arguments as it introduced a regression where cloudflared failed
  to read URLs in configuration files.
- cloudflared now logs the reason for failed connections if the error is recoverable.

## 2021.2.3

### Backward Incompatible Changes

- Removes db-connect. The Cloudflare Workers product will continue to support db-connect implementations with versions
  of cloudflared that predate this release and include support for db-connect.

### New Features

- Introduces support for proxy configurations with websockets in arbitrary TCP connections (#318).

### Improvements

- (reverted) Nested command line argument handling.

### Bug Fixes

- The maximum number of upstream connections is now limited by default which should fix reported issues of cloudflared
  exhausting CPU usage when faced with connectivity issues.
