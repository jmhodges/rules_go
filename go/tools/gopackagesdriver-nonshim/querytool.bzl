load("@io_bazel_rules_go//go:def.bzl", "go_context", "go_rule")

def _querytool_in_go_env(ctx):
    go = go_context(ctx)
    script_content = "#!/bin/bash\n\n"
    bashvars = []
    for var in sorted(go.env.keys()):
        bashvars.append("export %s=%s\n" % (var, go.env[var]))
    script_content += "".join(bashvars)
    script_content += ctx.file.gopackagesdriver.short_path +" $@\n"

    script = ctx.actions.declare_file("querytool.sh")
    ctx.actions.write(script, script_content, is_executable = True)
    
    runfiles = ctx.runfiles(files = [ctx.file.gopackagesdriver] + go.sdk.srcs + go.sdk.libs + go.sdk.headers)
    return DefaultInfo(
        executable = script,
        runfiles = runfiles,
    )

# This go_rule set up is to give genfakestdlib access to GOROOT so it can see
# what files are in each stdlib library.
querytool_in_go_env = go_rule(
    _querytool_in_go_env,
    attrs = {
        # FIXME it'd be nice for there to be a go_gen_rule because setting up
        # these attrs for what could entries in a genrule's tools, srcs, and
        # outputs fields are annoying.
        "gopackagesdriver": attr.label(
            doc = "The binary that generates the Go code of containing the stdlib maps",
            allow_single_file = True,
            default = ":gopackagesdriver-nonshim",
            executable = True,
            cfg = "host",
            mandatory = False,
        ),
    },
    executable = True,
)
