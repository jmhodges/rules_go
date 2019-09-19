load(
    "@io_bazel_rules_go//go/private:context.bzl",
    "go_context",
)
load(
    "@io_bazel_rules_go//go/private:providers.bzl",
    "GoBinary",
    "GoLibrary",
    "GoSource",
    "GoTest",
)

# FIXME unused. Ditch it?
GoPackagesFilesProvider = provider(
    doc = "Returns the ID, Name, PkgPath, Errors, GoFiles, and OtherFiles needed for go/packages.NeedFiles and NeedName modes.",
    fields = {
        "id": "The target (acting as unique identifier) of the Go package in bazel.",
        "name": "Name of the Go package as it appears in the package's source code",
        "pkg_path": "Package path as used by the go/types package",
        "errors": "Any errors encountered querying the metadata of the package, or while parsing or type-checking its files.",
        "go_files": "The absolute file paths of the package's Go source files (as seen after doing the rules_go mode processing).",
        "other_files": "The absolute file paths of the package's non-Go source files including assembly, C, C++, Fortran, Objective-C, SWIG, and so on (as seen after performing the rules_go mode processing)."
    },
)

def _gopackagesdriver_files_aspect_impl(target, ctx):
    go = go_context(ctx, ctx.rule.attr)

    source = target[GoSource] if GoSource in target else None
    if not GoLibrary in target:
        # Not a rule we can do anything with
        return []
    # We have a library and we need to compile it in a new mode
    library = target[GoLibrary]

    # FIXME rules_go question: is this method of getting pkg_name acceptable or do we need to do
    # more interrogation of the source?
    last_slash_index = library.importpath.rfind("/")
    pkg_name = library.importpath
    if last_slash_index != -1:
        pkg_name = pkg_name[last_slash_index+1:]
    go_srcs = []
    nongo_srcs = []

    # FIXME rules_go question: i suspect this isn't quite right. GoArchive mentions that orig_srcs
    # is more different kinds of files while srcs includes files that are output
    # after cgo or cover processing is done. 
    for src in source.srcs:
        if src.extension == "go":
            go_srcs.append(src.path)
        else:
            nongo_srcs.append(src.path)
    label_parts = []
    if target.label.workspace_name != "":
        label_parts.append("@"+target.label.workspace_name)
    label_parts.append("//"+target.label.package)
    label_parts.append(":"+target.label.name)
    label_string = "".join(label_parts)
    json_serialized = struct(
        id = label_string,
        name = pkg_name,
        pkg_path = library.importpath,
        go_files = go_srcs,
        other_fiels = nongo_srcs,
    ).to_json()

    # FIXME go_binary that embeds a go_library will cause the same contents
    # (like source file names) to be written into two different files under
    # different names. This will likely confuse tools, right? It's not clear how
    # to distinguish go_test from go_binary if we just ignore go_binary's and
    # we'd still have to handle the case where go_binary directly has the srcs
    # on it with no intermediary go_library that's been set up in `embed`.
    filename = "%s.gopackagesdriver_files_mode.json" % target.label.name
    json_file = ctx.actions.declare_file(filename)
    ctx.actions.write(json_file, json_serialized)

    return [OutputGroupInfo(
        gopackagesdriver_data = [json_file],
    )]


# gopackagesdriver_files_aspect returns the info about a bazel Go target that
# satisfies go/packages.Load's NeedName and NeedFiles LoadModes. It does not
# recurse.
gopackagesdriver_files_aspect = aspect(
    _gopackagesdriver_files_aspect_impl,
    attr_aspects = [],
    toolchains = ["@io_bazel_rules_go//go:toolchain"],
)
