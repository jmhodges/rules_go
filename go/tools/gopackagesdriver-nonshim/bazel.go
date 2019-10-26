package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"

	bespb "github.com/bazelbuild/rules_go/go/tools/gopackagesdriver-nonshim/proto/build_event_stream"
	"github.com/golang/protobuf/proto"
	"golang.org/x/tools/go/packages"
)

func bazelBuildAspects(mode packages.LoadMode, buildFlags []string, bazelTargets []packageID) (*proto.Buffer, error) {
	// Build package data files using bazel. We use one of several aspects
	// (depending on what mode we're in). The aspect produces .json and source
	// files in an output group. Each .json file contains a serialized
	// *packages.Package object.
	outputGroups := goPkgsDriverOutputGroup
	// FIXME allow overriding of the io_bazel_rules_go external name?
	aspect := "@io_bazel_rules_go//go:def.bzl%"
	// FIXME this needs to be regularized
	if (mode & packages.NeedDeps) != 0 {
		outputGroups += ",gopackagesdriver_archives,gopackagesdriver_deps_data"
		aspect += "gopackagesdriver_export"
	} else if mode&(packages.NeedCompiledGoFiles|packages.NeedExportsFile|packages.NeedImports) != 0 {
		outputGroups += ",gopackagesdriver_archives"
		aspect += "gopackagesdriver_export_nodeps"
	} else if mode&(packages.NeedName|packages.NeedFiles) != 0 {
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

	strTargs := make([]string, len(bazelTargets))
	for i, x := range bazelTargets {
		strTargs[i] = string(x)
	}
	cmd := exec.Command("bazel", "build")
	cmd.Args = append(cmd.Args, "--aspects="+aspect)
	cmd.Args = append(cmd.Args, "--output_groups="+outputGroups)
	cmd.Args = append(cmd.Args, "--build_event_binary_file="+eventFile.Name())
	cmd.Args = append(cmd.Args, buildFlags...)
	cmd.Args = append(cmd.Args, "--")
	cmd.Args = append(cmd.Args, strTargs...)

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

func extractBazelAspectOutputFilePaths(pbuf *proto.Buffer) (map[string]bool, error) {
	// FIXME I'm not sure what the goal of this variable and visit was for, but
	// I'm sure I'll find out soon.
	var rootSets []string
	setToFiles := make(map[string][]string)
	setToSets := make(map[string][]string)

	var event bespb.BuildEvent
	for !event.GetLastMessage() {
		if err := pbuf.DecodeMessage(&event); err != nil {
			return nil, fmt.Errorf("unable to parse bazel event message: %w", err)
		}

		if id := event.GetId().GetTargetCompleted(); id != nil {
			completed := event.GetCompleted()
			if !completed.GetSuccess() {
				return nil, fmt.Errorf("%s: target did not build successfully", id.GetLabel())
			}
			for _, g := range completed.GetOutputGroup() {
				if g.GetName() != goPkgsDriverOutputGroup {
					continue
				}
				for _, s := range g.GetFileSets() {
					if setId := s.GetId(); setId != "" {
						rootSets = append(rootSets, setId)
					}
				}
			}
		}

		if id := event.GetId().GetNamedSet(); id != nil {
			files := event.GetNamedSetOfFiles().GetFiles()
			fileNames := make([]string, len(files))
			for i, f := range files {
				u, err := url.Parse(f.GetUri())
				if err != nil {
					return nil, fmt.Errorf("unable to parse file URI %#v: %w", f.GetUri(), err)
				}
				if u.Scheme == "file" {
					fileNames[i] = u.Path
				} else {
					return nil, fmt.Errorf("scheme in bazel output files must be \"file\", but got %#v in URI %#v", u.Scheme, f.GetUri())
				}
			}
			setToFiles[id.GetId()] = fileNames
			sets := event.GetNamedSetOfFiles().GetFileSets()
			setIds := make([]string, len(sets))
			for i, s := range sets {
				setIds[i] = s.GetId()
			}
			setToSets[id.GetId()] = setIds
			continue
		}
	}
	var visit func(string, map[string]bool, map[string]bool)
	visit = func(setId string, files map[string]bool, visited map[string]bool) {
		if visited[setId] {
			return
		}
		visited[setId] = true
		for _, f := range setToFiles[setId] {
			files[f] = true
		}
		for _, s := range setToSets[setId] {
			visit(s, files, visited)
		}
	}

	files := make(map[string]bool)
	for _, s := range rootSets {
		visit(s, files, map[string]bool{})
	}
	return files, nil
}

// FIXME make it so we can conditionally print out all of the commands and args we exec to
// stderr.
func bazelQuery(args ...string) ([]byte, error) {
	newArgs := make([]string, 0, len(args)+1)
	newArgs = append(newArgs, "query")
	newArgs = append(newArgs, args...)
	return bazelOutput(newArgs...)
}

func bazelOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("bazel")
	cmd.Args = append(cmd.Args, args...)
	log.Println("1FIXME bazelQuery 002: bazel out", cmd.Args)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if eErr, ok := err.(*exec.ExitError); ok {
		eErr.Stderr = stderr.Bytes()
		err = &StderrExitError{Err: eErr}
	}
	// FIXME just always use os.Stderr?
	os.Stderr.Write(stderr.Bytes())
	if err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}
