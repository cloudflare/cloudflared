workspace(name = "com_zombiezen_go_capnproto2")

git_repository(
    name = "io_bazel_rules_go",
    remote = "https://github.com/bazelbuild/rules_go.git",
    commit = "43a3bda3eb97e7bcd86f564a1e0a4b008d6c407c",
)

load("@io_bazel_rules_go//go:def.bzl", "go_repositories", "go_repository")

go_repositories()

go_repository(
    name = "com_github_kylelemons_godebug",
    importpath = "github.com/kylelemons/godebug",
    sha256 = "4415b09bae90e41695bc17e4d00d0708e1f6bbb6e21cc22ce0146a26ddc243a7",
    strip_prefix = "godebug-a616ab194758ae0a11290d87ca46ee8c440117b0",
    urls = [
        "https://github.com/kylelemons/godebug/archive/a616ab194758ae0a11290d87ca46ee8c440117b0.zip",
    ],
)

go_repository(
    name = "org_golang_x_net",
    importpath = "golang.org/x/net",
    sha256 = "880dc04d0af397dce6875ee2349bbb4295fe5a47352f7a4da4270456f726edd4",
    strip_prefix = "net-f5079bd7f6f74e23c4d65efa0f4ce14cbd6a3c0f",
    urls = [
        "https://github.com/golang/net/archive/f5079bd7f6f74e23c4d65efa0f4ce14cbd6a3c0f.zip",
    ],
)
