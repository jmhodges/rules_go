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

# Dependency just for examples/buildless_external
new_go_repository(
    name = "com_github_jmhodges_remotecgorepo",
    importpath = "github.com/jmhodges/remotecgorepo",
    commit = "d5f572d8f6156cb51e4d8a08b07473e67c7d6b0d",
)

go_internal_tools_deps()

# Protocol buffers

load("//proto:go_proto_library.bzl", "go_proto_repositories")

go_proto_repositories(
    # DO NOT specify this internal attribute outside of rules_go project itself.
    rules_go_repo_only_for_internal_use = "@",
)
