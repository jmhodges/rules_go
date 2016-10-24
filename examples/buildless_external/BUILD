load("//go:def.bzl", "go_binary", "new_go_repository")

go_binary(
    name = "mathit",
    srcs = [
        "main.go",
    ],
    deps = [
        "@com_github_bazelbuild_rulesgo//examples/buildless_external/remotecgorepo:go_default_library",
    ],
)
