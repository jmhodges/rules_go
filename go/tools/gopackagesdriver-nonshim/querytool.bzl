load("@io_bazel_rules_go//go:def.bzl", "go_context", "go_rule")

def _get_runfile_path(ctx, f):
    """Return the runfiles relative path of f."""
    if ctx.workspace_name:
        return ctx.workspace_name + "/" + f.short_path
    else:
        return f.short_path

def _querytool_in_go_env(ctx):
    go = go_context(ctx)
    script_content = """#!/bin/bash

RUNFILES="${BASH_SOURCE[0]}.runfiles"
echo "RUNFILES is ${RUNFILES}" > /dev/stderr
# cd ${BASH_SOURCE[0]}.runfiles
"""
    bashvars = []
    for var in sorted(go.env.keys()):
        # FIXME i think this sets up GOOS and GOARCH's that are for the host
        # machine, and not the target machine. Think we'd benefit that
        # go_genrule that set ups toolchains and GOOS and GOARCH the way we need
        # them.
        if var != "PATH":
            bashvars.append("export %s=%s\n" % (var, go.env[var]))
    script_content += "".join(bashvars)
    script_content += """echo \"PWD is $PWD\" > /dev/stderr
${RUNFILES}/%s $@
""" %  _get_runfile_path(ctx, ctx.file.gopackagesdriver)
    # FIXME delete these
    # tmpl = ctx.actions.declare_file("querytool.sh.tmpl")
    # ctx.actions.write(tmpl, script_content
    
    script = ctx.actions.declare_file("querytool.sh")
    ctx.actions.write(script, script_content, is_executable = True)
    
    runfiles = ctx.runfiles(
        files = [ctx.file.gopackagesdriver] + go.sdk.srcs + go.sdk.libs + go.sdk.headers,
        collect_data = True,
    )
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
