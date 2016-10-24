load("//go:def.bzl", "go_binary", "new_go_repository")

go_test(
    name = "math_test",
    srcs = [
        "main_test.go",
    ],
    deps = [
        "@com_github_bazelbuild_rulesgo//examples/buildless_external/remotecgorepo:go_default_library",
    ],
)
