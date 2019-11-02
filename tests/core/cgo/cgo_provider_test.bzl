load("@bazel_skylib//lib:unittest.bzl", "asserts", "analysistest")
load("@io_bazel_rules_go//go:def.bzl", "go_library", "GoArchive")

def _provider_contents_test_impl(ctx):
    env = analysistest.begin(ctx)

    target_under_test = analysistest.target_under_test(env)
    data = target_under_test[GoArchive].data

    asserts.equals(env, len(data.srcs)-1, len(data.orig_srcs))
    asserts.equals(env, "_cgo_imports.go", data.srcs[len(data.srcs)-1].basename,)

    return analysistest.end(env)

provider_contents_test = analysistest.make(_provider_contents_test_impl)

def _test_provider_contents():
    go_library(
        name = "provider_contents_subject",
        srcs = [
            "add.c",
            "add.cpp",
            "add.h",
            "adder.go",
        ] + select({
            "@io_bazel_rules_go//go/platform:darwin": [
            "add.m",
                "add.mm",
            ],
            "//conditions:default": [],
        }),
        cgo = True,
        copts = ["-DRULES_GO_C"],
        cppopts = ["-DRULES_GO_CPP"],
        cxxopts = ["-DRULES_GO_CXX"],
        importpath = "github.com/bazelbuild/rules_go/tests/core/cxx",
        # Tagged as 'manual', because this target should not be built using
        # `:all` except as a dependency of the test.
        tags = ["manual"]
    )
    provider_contents_test(
        name = "provider_contents_test",
        target_under_test = ":provider_contents_subject",
    )

def cgo_test_suite(name):
    _test_provider_contents()
    native.test_suite(
        name = name,
        tests = [":provider_contents_test"],
    )
