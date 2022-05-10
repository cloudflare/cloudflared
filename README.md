# Cloudflare Tunnel client

Contains the command-line client for Cloudflare Tunnel, a tunneling daemon that proxies traffic from the Cloudflare network to your origins.
This daemon sits between Cloudflare network and your origin (e.g. a webserver). Cloudflare attracts client requests and sends them to you
via this daemon, without requiring you to poke holes on your firewall --- your origin can remain as closed as possible.
Extensive documentation can be found in the [Cloudflare Tunnel section](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps) of the Cloudflare Docs.
All usages related with proxying to your origins are available under `cloudflared tunnel help`.

You can also use `cloudflared` to access Tunnel origins (that are protected with `cloudflared tunnel`) for TCP traffic
at Layer 4 (i.e., not HTTP/websocket), which is relevant for use cases such as SSH, RDP, etc.
Such usages are available under `cloudflared access help`.

You can instead use [WARP client](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/configuration/private-networks)
to access private origins behind Tunnels for Layer 4 traffic without requiring `cloudflared access` commands on the client side.


## Before you get started

Before you use Cloudflare Tunnel, you'll need to complete a few steps in the Cloudflare dashboard: you need to add a
website to your Cloudflare account. Note that today it is possible to use Tunnel without a website (e.g. for private
routing), but for legacy reasons this requirement is still necessary:
1. [Add a website to Cloudflare](https://support.cloudflare.com/hc/en-us/articles/201720164-Creating-a-Cloudflare-account-and-adding-a-website)
2. [Change your domain nameservers to Cloudflare](https://support.cloudflare.com/hc/en-us/articles/205195708)


## Installing `cloudflared`

Downloads are available as standalone binaries, a Docker image, and Debian, RPM, and Homebrew packages. You can also find releases here on the `cloudflared` GitHub repository.

* You can [install on macOS](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#macos) via Homebrew or by downloading the [latest Darwin amd64 release](https://github.com/cloudflare/cloudflared/releases)
* Binaries, Debian, and RPM packages for Linux [can be found here](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#linux)
* A Docker image of `cloudflared` is [available on DockerHub](https://hub.docker.com/r/cloudflare/cloudflared)
* You can install on Windows machines with the [steps here](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#windows)

User documentation for Cloudflare Tunnel can be found at https://developers.cloudflare.com/cloudflare-one/connections/connect-apps


## Creating Tunnels and routing traffic

Once installed, you can authenticate `cloudflared` into your Cloudflare account and begin creating Tunnels to serve traffic to your origins.

* Create a Tunnel with [these instructions](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/create-tunnel)
* Route traffic to that Tunnel:
  * Via public [DNS records in Cloudflare](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/routing-to-tunnel/dns)
  * Or via a public hostname guided by a [Cloudflare Load Balancer](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/routing-to-tunnel/lb)
  * Or from [WARP client private traffic](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/private-net/)


## TryCloudflare

Want to test Cloudflare Tunnel before adding a website to Cloudflare? You can do so with TryCloudflare using the documentation [available here](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/run-tunnel/trycloudflare).

## Deprecated versions

Cloudflare currently supports versions of `cloudflared` 2020.5.1 and later. Breaking changes unrelated to feature availability may be introduced that will impact versions released prior to 2020.5.1. You can read more about upgrading `cloudflared` in our [developer documentation](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#updating-cloudflared).

| Version(s) | Deprecation status |
|---|---|
| 2020.5.1 and later | Supported |
| Versions prior to 2020.5.1 | No longer supported |
