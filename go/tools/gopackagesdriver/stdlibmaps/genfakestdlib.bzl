load("@io_bazel_rules_go//go:def.bzl", "go_context", "go_rule")

def _go_gen_stdlib_maps(ctx):
    go = go_context(ctx)
    print("FIXME go env is", go.env)
    print("FIXME go srcs is", go.sdk.srcs)
    print("FIXME just one is", go.sdk.srcs[0].path)
    go.actions.run_shell(
        outputs = [ctx.outputs.out],
        inputs = [go.sdk.package_list]+go.sdk.srcs,
        tools = [ctx.file.genfakestdlib],
        command = "{genfakestdlib} -pkglist {package_list} -goroot {goroot} > {out}".format(
            genfakestdlib = ctx.file.genfakestdlib.path,
            package_list = go.sdk.package_list.path,
            goroot = go.root,
            out = ctx.outputs.out.path,
        ),
        env = go.env,
    )

# This go_rule set up is to give genfakestdlib access to GOROOT so it can see
# what files are in each stdlib library.
gen_stdlib_maps = go_rule(
    _go_gen_stdlib_maps,
    attrs = {
        # FIXME it'd be nice for there to be a go_gen_rule because setting up
        # these attrs for what could entries in a genrule's tools, srcs, and
        # outputs fields are annoying.
        "genfakestdlib": attr.label(
            doc = "The binary that generates the Go code of containing the stdlib maps",
            allow_single_file = True,
            default = ":genfakestdlib",
            executable = True,
            cfg = "host",
            mandatory = False,
        ),
        "out": attr.output(
            doc = "The file name of new Go source file to put the generated Go code",
            mandatory = True,
        ),
    },
)
