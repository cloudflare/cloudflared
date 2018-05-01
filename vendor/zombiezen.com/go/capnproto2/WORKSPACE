workspace(name = "com_zombiezen_go_capnproto2")

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

http_archive(
    name = "io_bazel_rules_go",
    sha256 = "8b68d0630d63d95dacc0016c3bb4b76154fe34fca93efd65d1c366de3fcb4294",
    urls = ["https://github.com/bazelbuild/rules_go/releases/download/0.12.1/rules_go-0.12.1.tar.gz"],
)

http_archive(
    name = "bazel_gazelle",
    sha256 = "ddedc7aaeb61f2654d7d7d4fd7940052ea992ccdb031b8f9797ed143ac7e8d43",
    urls = ["https://github.com/bazelbuild/bazel-gazelle/releases/download/0.12.0/bazel-gazelle-0.12.0.tar.gz"],
)

load("@io_bazel_rules_go//go:def.bzl", "go_register_toolchains", "go_rules_dependencies")

go_rules_dependencies()

go_register_toolchains()

load("@bazel_gazelle//:deps.bzl", "gazelle_dependencies", "go_repository")

gazelle_dependencies()

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
