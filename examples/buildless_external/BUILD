package(
    default_visibility = ["//visibility:public"],
)

load("//go:def.bzl", "go_test")

go_test(
    name = "go_default_test",
    srcs = [
        "main_test.go",
    ],
    deps = [
        "@com_github_jmhodges_remotecgorepo//:go_default_library",
    ],
)
