load("@io_bazel_rules_go//go:def.bzl", "go_context", "go_rule")

def _get_runfile_path(ctx, f):
    """Return the runfiles relative path of f."""
    if ctx.workspace_name:
        return ctx.workspace_name + "/" + f.short_path
    else:
        return f.short_path

def _go_env_cmd_impl(ctx):
    go = go_context(ctx)
    tmpl = """#!/bin/bash

execPrefix=""
if [[ ! -z "${{BAZEL_EXEC_ROOT}}" ]]; then
  execPrefix="${{BAZEL_EXEC_ROOT}}/"
fi

export GOOS="{goos}"
export GOARCH="{goarch}"
export GOROOT="${{execPrefix}}/{goroot}"
export GO111MODULE="off"
export PATH="${{GOROOT}}/bin:$PATH"
echo "BAZEL_EXEC_ROOT is ${{BAZEL_EXEC_ROOT}}" > /dev/stderr
{cmd}
"""
    runfiles_raw = go.sdk.srcs + go.sdk.libs + go.sdk.headers
    for input_targ in ctx.attr.inputs:
        runfiles_raw.append(*input_targ.files.to_list())
    for tool_targ in ctx.attr.tools:
        runfiles_raw.append(*tool_targ.files.to_list())
    runfiles_raw.append(go.go)

    expandedcmd = ctx.expand_location(ctx.attr.cmd)
    fullcmd = tmpl.format(
        goos = go.sdk.goos,
        goarch = go.sdk.goarch,
        goroot = go.root,
        godir = go.go.path[:-1 - len(go.go.basename)],
        path = go.env["PATH"],
        cmd = expandedcmd,
    )
    out = ctx.outputs.out
    ctx.actions.write(out, fullcmd, is_executable = True)
    runfiles = ctx.runfiles(
        files = runfiles_raw,
    )
    return DefaultInfo(
        executable = out,
        runfiles = runfiles,
    )

# go_env_cmd generates a shell script that wraps cmd, the given bash command,
# with an environment that is suitable for Go tooling. It includes GOROOT (with
# all sources, libraries, and headers), GOARCH, and GOOS, modules are turned
# off, and the go binary is available in $PATH. If the built command is run
# outside of a `bazel run`, it requires the environment varaible BAZEL_EXEC_ROOT
# to be set to the value value of `bazel info execution_root` in order to use
# the correct `go` binary.
go_env_cmd = go_rule(
    _go_env_cmd_impl,
    attrs = {
        "cmd": attr.string(
            doc = "The bash command to run in an environment with GOROOT, GOOS, and GOARCH set and usuable.",
            mandatory = True,
        ),
        "tools": attr.label_list(
            doc = "A list of tool dependencies for this rule.",
            default = [],
            mandatory = False,
        ),
        "inputs": attr.label_list(
            doc = "A list of files required to run the given command that are not going to be executed by it.",
            default = [],
            mandatory = False,
        ),
        "out": attr.output(
            doc = "The file path to put the generated shell script that wraps the given cmd",
            mandatory = True,
        ),
    },
)

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
