package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/golang/protobuf/proto"
	"golang.org/x/tools/go/packages"
)

func bazelBuildAspects(req *driverRequest, bazelTargets []string) (*proto.Buffer, error) {
	// Build package data files using bazel. We use one of several aspects
	// (depending on what mode we're in). The aspect produces .json and source
	// files in an output group. Each .json file contains a serialized
	// *packages.Package object.
	outputGroups := goPkgsDriverOutputGroup
	// FIXME allow overriding of the io_bazel_rules_go external name?
	aspect := "@io_bazel_rules_go//go:def.bzl%"
	// FIXME this needs to be regularized
	if (req.Mode & packages.NeedDeps) != 0 {
		outputGroups += ",gopackagesdriver_archives"
		aspect += "gopackagesdriver_export"
	} else if req.Mode&(packages.NeedCompiledGoFiles|packages.NeedExportsFile|packages.NeedImports) != 0 {
		outputGroups += ",gopackagesdriver_archives"
		aspect += "gopackagesdriver_export_nodeps"
	} else if req.Mode&(packages.NeedName|packages.NeedFiles) != 0 {
		// FIXME possible to do these modes without actually building the
		// library? It's way slow on first access right now.
		aspect += "gopackagesdriver_files"
	} else {
		return nil, fmt.Errorf("unsupported packages.LoadModes set")
	}

	// We ask bazel to write build event protos to a binary file, which
	// we read to find the output files.
	eventFile, err := ioutil.TempFile("", "gopackagesdriver-bazel-bep-*.bin")
	if err != nil {
		return nil, fmt.Errorf("unable to create temporary file for storing bazel build output info: %w", err)
	}
	eventFileName := eventFile.Name()
	defer func() {
		if eventFile != nil {
			eventFile.Close()
		}
		os.Remove(eventFileName)
	}()

	cmd := exec.Command("bazel", "build")
	cmd.Args = append(cmd.Args, "--aspects="+aspect)
	cmd.Args = append(cmd.Args, "--output_groups="+outputGroups)
	cmd.Args = append(cmd.Args, "--build_event_binary_file="+eventFile.Name())
	cmd.Args = append(cmd.Args, req.BuildFlags...)
	cmd.Args = append(cmd.Args, "--")
	cmd.Args = append(cmd.Args, bazelTargets...)

	cmd.Stdout = os.Stderr // sic
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("error running bazel: %v", err)
	}

	eventData, err := ioutil.ReadAll(eventFile)
	if err != nil {
		return nil, fmt.Errorf("could not read bazel build event file: %v", err)
	}
	eventFile.Close()
	return proto.NewBuffer(eventData), nil
}
