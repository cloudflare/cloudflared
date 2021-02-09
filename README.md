# Argo Tunnel client

Contains the command-line client for Argo Tunnel, a tunneling daemon that proxies any local webserver through the Cloudflare network. Extensive documentation can be found in the [Argo Tunnel section](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps) of the Cloudflare Docs.

## Before you get started

Before you use Argo Tunnel, you'll need to complete a few steps in the Cloudflare dashboard. The website you add to Cloudflare will be used to route traffic to your Tunnel.

1. [Add a website to Cloudflare](https://support.cloudflare.com/hc/en-us/articles/201720164-Creating-a-Cloudflare-account-and-adding-a-website)
2. [Change your domain nameservers to Cloudflare](https://support.cloudflare.com/hc/en-us/articles/205195708)

## Installing `cloudflared`

Downloads are available as standalone binaries, a Docker image, and Debian, RPM, and Homebrew packages. You can also find releases here on the `cloudflared` GitHub repository.

* You can [install on macOS](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#macos) via Homebrew or by downloading the [latest Darwin amd64 release](https://github.com/cloudflare/cloudflared/releases)
* Binaries, Debian, and RPM packages for Linux [can be found here](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#linux)
* A Docker image of `cloudflared` is [available on DockerHub](https://hub.docker.com/r/cloudflare/cloudflared)
* You can install on Windows machines with the [steps here](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#windows)

User documentation for Argo Tunnel can be found at https://developers.cloudflare.com/cloudflare-one/connections/connect-apps

## Creating Tunnels and routing traffic

Once installed, you can authenticate `cloudflared` into your Cloudflare account and begin creating Tunnels that serve traffic for hostnames in your account.

* Create a Tunnel with [these instructions](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/create-tunnel)
* Route traffic to that Tunnel with [DNS records in Cloudflare](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/routing-to-tunnel/dns) or with a [Cloudflare Load Balancer](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/routing-to-tunnel/lb)

## TryCloudflare

Want to test Argo Tunnel before adding a website to Cloudflare? You can do so with TryCloudflare using the documentation [available here](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/trycloudflare).

## Deprecated versions

Cloudflare currently supports all versions of `cloudflared`. Starting on March 20, 2021, Cloudflare will no longer support versions released prior to 2020.5.1.

All features available in versions released prior to 2020.5.1 are available in current versions. Breaking changes unrelated to feature availability may be introduced that will impact versions released prior to 2020.5.1. You can read more about upgrading `cloudflared` in our [developer documentation](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#updating-cloudflared).

| Version(s) | Deprecation status |
|---|---|
| 2020.5.1 and later | Supported |
| Versions prior to 2020.5.1 | Will no longer be supported starting March 20, 2021 |
