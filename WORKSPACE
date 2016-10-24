workspace(name = "io_bazel_rules_go")

load("//go:def.bzl", "go_repositories", "new_go_repository")
load("//go/private:go_repositories.bzl", "go_internal_tools_deps")

go_repositories(
    # DO NOT specify this internal attribute outside of rules_go project itself.
    rules_go_repo_only_for_internal_use = "@",
)

new_go_repository(
    name = "com_github_golang_glog",
    commit = "23def4e6c14b4da8ac2ed8007337bc5eb5007998",
    importpath = "github.com/golang/glog",
    # DO NOT specify this internal attribute outside of rules_go project itself.
    rules_go_repo_only_for_internal_use = "@",
)

# Self-dependency for the examples/buildless_external example
new_go_repository(
    name = "io_bazel_rules_go",
    importpath = "github.com/jmhodges/rules_go",
    commit = "4ed73f44a60f1bf96832702d67521e96dde67025",
)

go_internal_tools_deps()

# Protocol buffers

load("//proto:go_proto_library.bzl", "go_proto_repositories")

go_proto_repositories(
    # DO NOT specify this internal attribute outside of rules_go project itself.
    rules_go_repo_only_for_internal_use = "@",
)
