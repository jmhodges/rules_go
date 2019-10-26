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

def _gopackagesdriver_files_nodeps_aspect_impl(target, ctx):
    go = go_context(ctx, ctx.rule.attr)

    source = target[GoSource] if GoSource in target else None
    if not GoLibrary in target:
        # Not a rule we can do anything with
        return []
    # We have a library and we need to compile it in a new mode
    library = target[GoLibrary]
    resp = _basic_driver_response(ctx, target, source, library)

    json_serialized = struct(**resp).to_json()
    # FIXME go_binary that embeds a go_library will cause the same contents
    # (like source file names) to be written into tbwo different files under
    # different names. This will likely confuse tools, right? It's not clear how
    # to distinguish go_test from go_binary if we just ignore go_binary's and
    # we'd still have to handle the case where go_binary directly has the srcs
    # on it with no intermediary go_library that's been set up in `embed`.
    filename = "%s.files_nodeps_aspect.gopackagesdriver.json" % target.label.name
    json_file = ctx.actions.declare_file(filename)
    ctx.actions.write(json_file, json_serialized)

    return [OutputGroupInfo(
        gopackagesdriver_data = [json_file],
    )]

def _gopackagesdriver_export_nodeps_aspect_impl(target, ctx):
    source = target[GoSource] if GoSource in target else None
    if not GoLibrary in target:
        # Not a rule we can do anything with
        return []
    go = go_context(ctx, ctx.rule.attr)
    # We have a library and we need to compile it in a new mode
    library = target[GoLibrary]
    resp = _basic_driver_response(ctx, target, source, library)
    archive = target[GoArchive]
    export_resp = _export_driver_response(go, target.label, archive)
    resp.update(**export_resp)
    json_serialized = struct(**resp).to_json()

    filename = "%s.export_nodeps_aspect.gopackagesdriver.json" % target.label.name
    json_file = ctx.actions.declare_file(filename)
    ctx.actions.write(json_file, json_serialized)
    archives = [archive.data.file]

    return [
        OutputGroupInfo(
            gopackagesdriver_archives = archives,
            gopackagesdriver_data = [json_file],
        ),
    ]

# FIXME unused. Ditch it?
GoPackagesFilesProvider = provider(
    doc = "Returns the serialized JSON files for packages to be parsed as aspectResponse.",
    fields = {
        "pkg": "The aspectResponse information about the target (that is also stored in the serialized JSON). Used to walk the dependencies."
    },
)

def _gopackagesdriver_export_aspect_impl(target, ctx):
    source = target[GoSource] if GoSource in target else None
    if not GoLibrary in target:
        # Not a rule we can do anything with
        return None

    go = go_context(ctx, ctx.rule.attr)

    # FIXME remove this check_dep and do it as a loop
    resp = _gopackagesdriver_export_aspect_impl_go(target, ctx, go)
    if resp == None:
        return []
    archives = []
    deps_data = []
    imports = {}
    resp["imports"] = imports
    for targ in ctx.rule.attr.deps:
        gopack = targ[GoPackagesFilesProvider]
        if gopack == None:
            continue
        imports[gopack.pkg["id"]] = gopack.pkg

    json_serialized = struct(**resp).to_json()

    filename = "%s.export_aspect.gopackagesdriver.json" % target.label.name
    json_file = ctx.actions.declare_file(filename)
    ctx.actions.write(json_file, json_serialized)

    archives.append(target[GoArchive].data.file)

    return [
        # FIXME there has to be a better way to get these files to be dropped on
        # to disk than specifying OutputGroupInfo on top of our own
        # GoPackagesFilesProvider?
        OutputGroupInfo(
            gopackagesdriver_archives = archives,
            gopackagesdriver_data = [json_file],
        ),
        GoPackagesFilesProvider(
            pkg = resp
        ),
    ]

    
def _gopackagesdriver_export_aspect_impl_go(target, ctx, go):
    source = target[GoSource] if GoSource in target else None
    if not GoLibrary in target:
        # Not a rule we can do anything with
        return None
    # We have a library and we need to compile it in a new mode
    library = target[GoLibrary]
    resp = _basic_driver_response(ctx, target, source, library)
    archive = target[GoArchive]
    export_resp = _export_driver_response(go, target.label, archive)
    resp.update(**export_resp)

    return resp

def _label_to_string(label):
    label_parts = []
    if label.workspace_name != "":
        label_parts.append("@"+label.workspace_name)
    label_parts.append("//"+label.package)
    label_parts.append(":"+label.name)
    label_string = "".join(label_parts)
    return label_string


def _basic_driver_response(ctx, target, source, library):
    # FIXME rules_go question: is this method of getting pkg_name acceptable or do we need to do
    # more interrogation of the source?

    pkg_name = library.importpath
    if library.is_main:
        pkg_name = "main"
    else:
        last_slash_index = library.importpath.rfind("/")
        if last_slash_index != -1:
            pkg_name = pkg_name[last_slash_index+1:]

    go_srcs = []
    nongo_srcs = []

    # FIXME we're going to need to dig into GoCompilePkg to get the
    # cgo-generated intermediate go files and this extension check might not
    # work for those (and other similar generated go files, possibly).
    for src in source.srcs:
        if src.extension == "go":
            go_srcs.append(src.path)
        else:
            nongo_srcs.append(src.path)

    label_string = _label_to_string(target.label)
    deps = {}
    for deptarg in ctx.rule.attr.deps:
        deps[deptarg[GoLibrary].importpath] = _label_to_string(deptarg.label)
        
    return {
        "id": label_string,
        "name": pkg_name,
        "pkg_path": library.importpath, # FIXME maybe not right? maybe only from
                                       # the other _export aspect?
        "go_files": go_srcs,
        "other_files": nongo_srcs,
        "dep_importpaths_to_labels": deps,
    }

def _export_driver_response(go, target_label, archive):
    if go.nogo == None:
        # FIXME wait, didn't we fix this so we don't need nogo anymore because
        # we use the normal archive? check!
        fail(msg = "a nogo target must be passed to `go_register_toolchains` with at least `vet = True` or some other analysis tool in place in order to get type check export data requested by this aspect")
    if archive.data.export_file == None:
        fail(msg = "out_export wasn't set on given GoArchive for %s" % target_label)

    compiled_go_files = []
    for src in archive.data.srcs:
        compiled_go_files.append(src.path)

    return {
        "compiled_go_files": compiled_go_files,
        "export_file": archive.data.file.path,
    }

# gopackagesdriver_files_aspect returns the info about a bazel Go target that
# satisfies go/packages.Load's NeedName and NeedFiles LoadModes. It does not
# recurse.
gopackagesdriver_files_nodeps_aspect = aspect(
    _gopackagesdriver_files_nodeps_aspect_impl,
    attr_aspects = [],
    toolchains = ["@io_bazel_rules_go//go:toolchain"],
    required_aspect_providers = ["GoLibrary", "GoSource", "GoArchive", "GoArchiveData"],
    # FIXME set up `provides` arg
)

# gopackagesdriver_files_aspect returns the info about a bazel Go target that
# satisfies go/packages.Load's NeedCompiledGoFiles and NeedExportsFile
# LoadModes. It does not recurse.
gopackagesdriver_export_nodeps_aspect = aspect(
    _gopackagesdriver_export_nodeps_aspect_impl,
    toolchains = ["@io_bazel_rules_go//go:toolchain"],
    required_aspect_providers = ["GoLibrary", "GoSource", "GoArchive", "GoArchiveData"],
)

gopackagesdriver_export_aspect = aspect(
    _gopackagesdriver_export_aspect_impl,
    toolchains = ["@io_bazel_rules_go//go:toolchain"],
    attr_aspects = ["deps"],
    required_aspect_providers = ["GoLibrary", "GoSource", "GoArchive", "GoArchiveData"],
)

# FIXME delete this
def _debug_impl(target, ctx):
    go = go_context(ctx, ctx.rule.attr)

    print("FIXME 001 GoSource", target[GoSource])
    print("FIXME 002 GoArchive", target[GoArchive])
    print("FIXME 003 GoArchiveData", target[GoArchive].data)
    # foobar = ctx.actions.declare_file("foobar")

    # ctx.actions.run_shell(
    #     outputs = [foobar],
    #     inputs = [go.sdk.root_file],
    #     tools = [go.go],
    #     command = ctx.expand_location("echo $(execpath @go_sdk//:builtin/builtin.go) > foobar && echo FIXME4"),
    #     env = go.env,
    # )
    print("FIXME 050 libs", go.sdk.libs)
    print("FIXME 051 srcs", go.sdk.srcs)
    return [] # [OutputGroupInfo(welp=[foobar])]

debug_aspect = aspect( # FIXME delete this aspect
    _debug_impl,
    attr_aspects = [],
    toolchains = ["@io_bazel_rules_go//go:toolchain"],
)
