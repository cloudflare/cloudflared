# How to contribute

As of 2020-09-13, Ross Light, the primary maintainer, does not have enough time
to fix issues for this project or continue feature development. He can review
PRs to fix issues, answer questions, or opine on discussions, but he will not
address new issues. If you're interested in maintaining this project, please
reach out to Ross at ross@zombiezen.com. Thanks!

That said, we'd be happy to accept your patches to this project. There are a
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
