load(
    "@io_bazel_rules_go//go/private:context.bzl",
    "go_context",
)
load(
    "@io_bazel_rules_go//go/private:providers.bzl",
    "GoArchive",
    "GoLibrary",
    "GoSource",
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
    resp = _basic_driver_response(target, source, library)

    json_serialized = struct(**resp).to_json()
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

def _gopackagesdriver_export_aspect_impl(target, ctx):
    go = go_context(ctx, ctx.rule.attr)

    source = target[GoSource] if GoSource in target else None
    if not GoLibrary in target:
        # Not a rule we can do anything with
        return []
    # We have a library and we need to compile it in a new mode
    library = target[GoLibrary]
    resp = _basic_driver_response(target, source, library)
    export_resp = _export_driver_response(go, target, source)
    resp.update(**export_resp)
    json_serialized = struct(**resp).to_json()

    filename = "%s.gopackagesdriver_export_mode.json" % target.label.name
    json_file = ctx.actions.declare_file(filename)
    ctx.actions.write(json_file, json_serialized)

    return [OutputGroupInfo(
        gopackagesdriver_data = [json_file],
    )]


def _basic_driver_response(target, source, library):
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
    roots = []
    if target.label.workspace_name != "":
        label_parts.append("@"+target.label.workspace_name)
        roots.append(target.label.workspace_root)
    label_parts.append("//"+target.label.package)
    label_parts.append(":"+target.label.name)
    label_string = "".join(label_parts)

    return {
        "id": label_string,
        "name": pkg_name,
        "pkg_path": library.importpath, # FIXME maybe not right? maybe only from
                                       # the other _export aspect?
        "go_files": go_srcs,
        "other_files": nongo_srcs,
        "roots": [label_string],
    }

def _export_driver_response(go, target, source):
    archive = target[GoArchive]
    if archive == None:
        archive = go.archive(source)
    compiled_go_sources = []
    for src in archive.data.srcs:
        compiled_go_sources.append(src.path)
    print("FIXME archive.data: ", archive.data, "\narchive.data.srcs: ", archive.data.srcs)
    # FIXME updated this resp
    return {
        "compiled_go_sources": compiled_go_sources,
    }

# gopackagesdriver_files_aspect returns the info about a bazel Go target that
# satisfies go/packages.Load's NeedName and NeedFiles LoadModes. It does not
# recurse.
gopackagesdriver_files_aspect = aspect(
    _gopackagesdriver_files_aspect_impl,
    attr_aspects = [],
    toolchains = ["@io_bazel_rules_go//go:toolchain"],
    # FIXME set up `provides` arg
)

# gopackagesdriver_files_aspect returns the info about a bazel Go target that
# satisfies go/packages.Load's NeedCompiledGoFiles and NeedExportsFile
# LoadModes. It does not recurse.
gopackagesdriver_export_aspect = aspect(
    _gopackagesdriver_export_aspect_impl,
    attr_aspects = [],
    toolchains = ["@io_bazel_rules_go//go:toolchain"],
    # FIXME set up `provides` arg
)
