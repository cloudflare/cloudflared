# How to contribute

We'd love to accept your patches and contributions to this project. There are a
just a few small guidelines you need to follow.

## Code contributions

All submissions, including submissions by project members, require review. We
use GitHub pull requests for this purpose. Consult [GitHub Help][] for more
information on using pull requests.

When you make your first submission to this repository, please add your name to
the AUTHORS and CONTRIBUTORS file as part of your first pull request.

As a policy, go-capnproto2 should always build cleanly and pass tests on every
commit.  We run a [Travis build][] that checks before and after merges to
enforce this policy.  However, as a courtesy to other contributors, please run
`go test ./...` before sending a pull request (this is what the Travis
build does).

[GitHub Help]: https://help.github.com/articles/about-pull-requests/
[Travis build]: https://travis-ci.org/capnproto/go-capnproto2
