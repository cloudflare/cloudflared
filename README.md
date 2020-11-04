# Argo Tunnel client

Contains the command-line client and its libraries for Argo Tunnel, a tunneling daemon that proxies any local webserver through the Cloudflare network. Extensive documentation can be found in the [Argo Tunnel section](https://developers.cloudflare.com/argo-tunnel/) of the Cloudflare Docs.

## Before you get started

Before you use Argo Tunnel, you'll need to complete a few steps in the Cloudflare dashboard. The website you add to Cloudflare will be used to route traffic to your Tunnel.

1. [Add a website to Cloudflare](https://support.cloudflare.com/hc/en-us/articles/201720164-Creating-a-Cloudflare-account-and-adding-a-website)
2. [Change your domain nameservers to Cloudflare](https://support.cloudflare.com/hc/en-us/articles/205195708)

## Installation

Downloads are available as standalone binaries, a Docker image, package managers like Homebrew, or Debian and RPM packages. You can also find releases here on the `cloudflared` GitHub repository.

* You can [install on macOS](https://developers.cloudflare.com/argo-tunnel/getting-started/installation#macos) via Homebrew or by downloading the latest Darwin amd64 release
* Binaries, Debian, and RPM packages for Linux [can be found here](https://developers.cloudflare.com/argo-tunnel/getting-started/installation#linux)
* A Docker image of `cloudflared` is [available on DockerHub](https://hub.docker.com/r/cloudflare/cloudflared)
* You can install on Windows machines with the [steps here](https://developers.cloudflare.com/argo-tunnel/getting-started/installation#windows)

User documentation for Argo Tunnel can be found at https://developers.cloudflare.com/argo-tunnel/

## Create Tunnels and routing traffic

Once installed, you can authenticate `cloudflared` into your Cloudflare account and begin creating Tunnels that serve traffic for hostnames in your account.

* Create a Tunnel with [these instructions](https://developers.cloudflare.com/argo-tunnel/create-tunnel)
* Route traffic to that Tunnel with [DNS records in Cloudflare](https://developers.cloudflare.com/argo-tunnel/routing-to-tunnel/dns)
* Route traffic to that Tunnel with a [Cloudflare Load Balancer](https://developers.cloudflare.com/argo-tunnel/routing-to-tunnel/lb)

## TryCloudflare

Want to test Argo Tunnel before adding a website to Cloudflare? You can do so with TryCloudflare using the documentation [available here](https://developers.cloudflare.com/argo-tunnel/learning/trycloudflare).
