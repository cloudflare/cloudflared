# How to use This fork 

## Compiling `cloudflared-notify` from source

- To compile this project you need to have ```build-essential(for debian)```/```base-devel(for arch)```/```OR euivalent package for your distro``` and ```go``` and ```python``` installed  in your system.
+ Clone or download this repo into your machine and go inside the ***cloudflared-notify*** folder & open a terminal in this folder location and type ```make cloudflared```, if successful then this will create a executable named **```cloudflared```**.
* This cloudflared executable can now be used as you would normally use the cloudflared executable.

> [!Note]
> Please note this fork is the same as normal cloudflare-tunnel, you need not to use another cloudflare-tunnel app if you are using this fork


## Using `cloudflared-notify`

#### It uses gmail smtp for sending out mail as for now, other mail services will be added in future (Mention your suggested mail server if any by opening an issue)

- To use this forked version of cloudflare tunnel notification functionality, you need to have a gmail account.

+ Now create an app password for your gmail account, [read instructions here on how to create app password](https://support.google.com/accounts/answer/185833?hl=en)

* Finally to take advantage of this notification functionality, run the cloudflared executable with this following commandline arguments:


```sh
cloudflared tunnel --url http://localhost:6009 --notify receipeint@mailID --uname login@mailID --key 16-digit-app-password-for-login@mailID
```


## Why this fork
This specific fork is for you if you are like me and is too poor to buy a domain *\*just kidding I know you make six-figures yearly ;)\**.
<br>
*(I know about freenom, but as for now freenom is not allowing to register new free domains 🤧🤧🤧🤧🤧)*
<br>
<br>
Let's consider my use case, my ISP charges a huge sum of money monthly for static IP, which is not feasable for me as a student, also my home server goes whereever I go, thats why static IP is of no use for me.
<br>
<br>
Then I stumpled upon **cloudflared**, \*my dream come true scenerio\*, but then comes the problem of buying & adding a domain in cloudflare if I want the url to be persistent and known to me, which is also not feasible for me *(because why pay money when you know how to reverse engineer & edit opensource code 😎😎 \*wallet sadness intensifies\*)* 
<br>
<br>
So I finalized the decision of using cloudflare quick tunnels, but the link reset every time my cloudflared service restarts.
<br>
And to know the new link every time the cloudflared service restarts I make this fork, that notifies the user via email the newly created quick tunnel link.
<br>
<br>
Now I dont need to physically go into my home server and fetch the quick tunnel link every time the cloudflared service restarts, I just get the link delivered in my mail box like a nerd 😎😎.
<br>
<br>
**I hope, you as user find this feature useful, and a huge credit goes to the team behind cloudflare-tunnel for making the cloudflared project opensource and letting developers like us in making the software better for every taste.**


# Cloudflared-notify Tunnel client

Contains the forked command-line client for Cloudflare Tunnel, a tunneling daemon that proxies traffic from the Cloudflare network to your origins.
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

Downloads are available as standalone binaries, a Docker image, and Debian, RPM, and Homebrew packages. You can also find releases [here](https://github.com/cloudflare/cloudflared/releases) on the `cloudflared` GitHub repository.

* You can [install on macOS](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#macos) via Homebrew or by downloading the [latest Darwin amd64 release](https://github.com/cloudflare/cloudflared/releases)
* Binaries, Debian, and RPM packages for Linux [can be found here](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#linux)
* A Docker image of `cloudflared` is [available on DockerHub](https://hub.docker.com/r/cloudflare/cloudflared)
* You can install on Windows machines with the [steps here](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#windows)
* Build from source with the [instructions here](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation#build-from-source)

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

Cloudflare currently supports versions of cloudflared that are **within one year** of the most recent release. Breaking changes unrelated to feature availability may be introduced that will impact versions released more than one year ago. You can read more about upgrading cloudflared in our [developer documentation](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/#updating-cloudflared).

For example, as of January 2023 Cloudflare will support cloudflared version 2023.1.1 to cloudflared 2022.1.1.
