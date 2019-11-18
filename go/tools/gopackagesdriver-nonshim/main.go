// Copyright 2019 The Bazel Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// gopackagesdriver collects metadata, syntax, and type information for
// Go packages built with bazel. It implements the driver interface for
// golang.org/x/tools/go/packages. When gopackagesdriver is installed
// in PATH, tools like gopls written with golang.org/x/tools/go/packages,
// work in bazel workspaces.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/types"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	bespb "github.com/bazelbuild/rules_go/go/tools/gopackagesdriver-nonshim/proto/build_event_stream"
	"github.com/bazelbuild/rules_go/go/tools/gopackagesdriver-nonshim/stdlibmaps"
	"github.com/golang/protobuf/proto"
	"golang.org/x/tools/go/packages"
)

// FIXME remove. just for debugging
type modeInfo struct {
	Name string
	Mode packages.LoadMode
}

var modes = []modeInfo{
	{"NeedName", packages.NeedName},

	// NeedFiles adds GoFiles and OtherFiles.
	{"NeedFiles", packages.NeedFiles},

	// NeedCompiledGoFiles adds CompiledGoFiles.
	{"NeedCompiledGoFiles", packages.NeedCompiledGoFiles},

	// NeedImports adds Imports. If NeedDeps is not set, the Imports field will contain
	// "placeholder"" Packages with only the ID set.
	{"NeedImports", packages.NeedImports},

	// NeedDeps adds the fields requested by the LoadMode in the packages in Imports.
	{"NeedDeps", packages.NeedDeps},

	// NeedExportsFile adds ExportsFile.
	{"NeedExportsFile", packages.NeedExportsFile},

	// NeedTypes adds Types, Fset, and IllTyped.
	{"NeedTypes", packages.NeedTypes},

	// NeedSyntax adds Syntax.
	{"NeedSyntax", packages.NeedSyntax},

	// NeedTypesInfo adds TypesInfo.
	{"NeedTypesInfo", packages.NeedTypesInfo},

	// NeedTypesSizes adds TypesSizes.
	{"NeedTypesSizes", packages.NeedTypesSizes},
}

func main() {
	f, err := os.OpenFile("/Users/jeffhodges/Desktop/wut.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("couldn't open log file: %s", err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.Println("FIXME main 001: targets", os.Args)
	log.SetPrefix("gopackagesdriver: ")
	log.SetFlags(0)
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

// driverRequest is a JSON object sent by golang.org/x/tools/go/packages
// on stdin. Keep in sync.
type driverRequest struct {
	Command    string            `json:"command"` // FIXME ???
	Mode       packages.LoadMode `json:"mode"`
	Env        []string          `json:"env"`         // FIXME handle
	BuildFlags []string          `json:"build_flags"` // FIXME handle
	Tests      bool              `json:"tests"`       // FIXME handle
	Overlay    map[string][]byte `json:"overlay"`     // FIXME handle
}

// driverResponse is a JSON object sent by this program to
// golang.org/x/tools/go/packages on stdout. Keep in sync.
type driverResponse struct {
	// Sizes, if not nil, is the types.Sizes to use when type checking.
	Sizes *types.StdSizes

	// Roots is the set of package IDs that make up the root packages.
	// We have to encode this separately because when we encode a single package
	// we cannot know if it is one of the roots as that requires knowledge of the
	// graph it is part of.
	Roots []string `json:",omitempty"`

	// Packages is the full set of packages in the graph.
	// The packages are not connected into a graph.
	// The Imports if populated will be stubs that only have their ID set.
	// Imports will be connected and then type and syntax information added in a
	// later pass (see refine).
	Packages []*packages.Package
}

const fileQueryPrefix = "file="

var (
	pwd = os.Getenv("PWD")
)

func run(args []string) error {
	// Parse command line arguments and driver request sent on stdin.
	fs := flag.NewFlagSet("gopackagesdriver", flag.ExitOnError)
	// FIXME figure out how to set a --platforms call?
	if err := fs.Parse(args); err != nil {
		return err
	}
	patterns := fs.Args()

	if len(patterns) == 0 {
		// FIXME double check this. a comment in go/packages's goListDriver
		// mentions that no patterns at all means to query for ".". I'm not sure
		// if that would be possible to do in bazel-land, but I'm going to leave
		// this FIXME instead of thinking about it too much.
		return errors.New("no patterns specified")
	}
	var targets, fileQueries []string
	for _, patt := range patterns {
		if strings.HasPrefix(patt, fileQueryPrefix) {
			fp := strings.TrimPrefix(patt, fileQueryPrefix)
			if len(fp) == 0 {
				return fmt.Errorf("\"file=\" prefix given with no query after it")
			}
			fileQueries = append(fileQueries, fp)
		} else {
			targets = append(targets, patt)
		}
	}

	reqData, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	req := &driverRequest{}
	if err := json.Unmarshal(reqData, &req); err != nil {
		return fmt.Errorf("could not unmarshal driver request: %v", err)
	}
	log.Println("FIXME driverRequest Modes 001")
	for _, m := range modes {
		if req.Mode&m.Mode != 0 {
			log.Println("FIXME 002 mode:", m.Name)
		}
	}
	var resp *driverResponse
	if len(fileQueries) != 0 {
		fileTargs, err := bazelTargetsFromFileQueries(req, fileQueries)
		if err != nil {
			return err
		}
		targets = append(targets, fileTargs...)
	}
	if len(targets) != 0 {
		resp, err = packagesFromBazelTargets(req, targets)
		if err != nil {
			return err
		}
	}
	respData, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("could not marshal driver response: %v", err)
	}
	_, err = os.Stdout.Write(respData)
	if err != nil {
		return err
	}

	return nil

}

func bazelTargetsFromFileQueries(req *driverRequest, fileQueries []string) ([]string, error) {
	var targets []string
	for _, fp := range fileQueries {
		fileLabel, err := filePathToLabel(fp)
		if err != nil {
			// FIXME Errors on driverResponse?
			return nil, err
		}
		targs, err := fileLabelToBazelTargets(fileLabel, fp)
		if err != nil {
			return nil, err
		}
		targets = append(targets, targs...)
	}
	return targets, nil
}

// StderrExitError wraps *exec.ExitError and prints the complete stderr output
// from a command.
type StderrExitError struct {
	Err *exec.ExitError
}

func (e *StderrExitError) Error() string {
	sb := &strings.Builder{}
	sb.Write(e.Err.Stderr)
	sb.WriteString(e.Err.Error())
	return sb.String()
}

func filePathToLabel(fp string) (string, error) {
	// FIXME handle ~ and remove the PWD prefix if its an absolute path and
	// reject if PWD isn't a prefix of the absolute path
	fp = filepath.Clean(fp)
	if filepath.IsAbs(fp) {
		if !strings.HasPrefix(fp, pwd) {
			return "", fmt.Errorf("error converting filepath %#v to bazel file label: filepath is absolute but the file doesn't exist in the tree below the current working directory", fp)
		}
		fp = strings.TrimPrefix(fp, pwd+"/")
	}
	bs, err := bazelQuery(fp)
	if err != nil {
		return "", fmt.Errorf("error converting filepath %v to bazel file label: %w", fp, err)
	}
	return string(bytes.TrimSpace(bs)), nil
}

func fileLabelToBazelTargets(label, origFile string) ([]string, error) {
	ind := strings.Index(label, ":")
	if ind == -1 {
		return nil, fmt.Errorf("no \":\" in file label %#v to be found in bazel targets", label)
	}
	packageSplat := label[:ind+1] + "*"
	bs, err := bazelQuery(fmt.Sprintf("attr(\"srcs\", %s, %s)", label, packageSplat))
	if err != nil {
		return nil, fmt.Errorf("error bazel file label %#v to bazel target: %w", label, err)
	}
	bbs := bytes.Split(bs, []byte{'\n'})
	targs := make([]string, 0, len(bbs))
	for _, line := range bbs {
		if len(line) == 0 {
			continue
		}
		targs = append(targs, string(line))
	}
	if len(targs) == 0 {
		return nil, fmt.Errorf("no targets in %#v contains the source file %#v", label[ind+1:], origFile)
	}
	return targs, nil
}

// FIXME make it so we can conditionally print out all of the commands and args we exec to
// stderr.
func bazelQuery(args ...string) ([]byte, error) {
	cmd := exec.Command("bazel", "query")
	cmd.Args = append(cmd.Args, args...)
	log.Println("1FIXME bazelQuery 002: bazel query", cmd.Args)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if eErr, ok := err.(*exec.ExitError); ok {
		eErr.Stderr = stderr.Bytes()
		err = &StderrExitError{Err: eErr}
	}
	if err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

const goPkgsDriverOutputGroup = "gopackagesdriver_data"

// FIXME only really supports one target
func packagesFromBazelTargets(req *driverRequest, targets []string) (*driverResponse, error) {
	log.Println("FIXME packagesFromBazelTargets 001: targets", targets)
	// Build package data files using bazel. We use one of several aspects
	// (depending on what mode we're in). The aspect produces .json and source
	// files in an output group. Each .json file contains a serialized
	// *packages.Package object.
	outputGroups := goPkgsDriverOutputGroup + ",gopackagesdriver_archives"
	// FIXME allow overriding of the io_bazel_rules_go external name?
	aspect := "@io_bazel_rules_go//go:def.bzl%"
	// FIXME this needs to be regularized
	if req.Mode&(packages.NeedCompiledGoFiles|packages.NeedExportsFile|packages.NeedDeps|packages.NeedImports) != 0 {
		aspect += "gopackagesdriver_export"
	} else if req.Mode&(packages.NeedName|packages.NeedFiles) != 0 {
		aspect += "gopackagesdriver_files"
	} else {
		return nil, fmt.Errorf("unsupported packages.LoadModes set")
	}

	// We ask bazel to write build event protos to a binary file, which
	// we read to find the output files.
	eventFile, err := ioutil.TempFile("", "gopackagesdriver-bazel-bep-*.bin")
	if err != nil {
		return nil, err
	}
	eventFileName := eventFile.Name()
	defer func() {
		if eventFile != nil {
			eventFile.Close()
		}
		os.Remove(eventFileName)
	}()

	bazelTargets := make([]string, 0, len(targets))
	var stdlibPatterns []string
	for _, targ := range targets {
		if imp, ok := stdlibmaps.StdlibBazelLabelToImportPath[targ]; ok {
			stdlibPatterns = append(stdlibPatterns, imp)
		} else if _, ok := stdlibmaps.StdlibImportPathToBazelLabel[targ]; ok {
			stdlibPatterns = append(stdlibPatterns, targ)
		} else {
			bazelTargets = append(bazelTargets, targ)
		}
	}

	// FIXME handle len(targets) == 0 explicilty. right now it's just a bunch of
	// code to move around, so I'm skipping it since it's just a warning from
	// bazel.
	log.Println("FIXME packagesFromBazelTargets 010 bazelTargets:", bazelTargets, "stdlibPatterns:", stdlibPatterns)
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

	// FIXME I'm not sure what the goal of this variable and visit was for, but
	// I'm sure I'll find out soon.
	var rootSets []string
	setToFiles := make(map[string][]string)
	setToSets := make(map[string][]string)
	pbuf := proto.NewBuffer(eventData)
	var event bespb.BuildEvent
	for !event.GetLastMessage() {
		if err := pbuf.DecodeMessage(&event); err != nil {
			return nil, err
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
					return nil, fmt.Errorf("unable to parse file URI %#v: %s", f.GetUri(), err)
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

	pkgs := make(map[string]*packages.Package)
	roots := make(map[string]bool)
	for fp, _ := range files {
		resp, err := parseAspectResponse(fp)
		if err != nil {
			return nil, fmt.Errorf("unable to parse JSON response in file %#v from aspect %#v: %s", fp, aspect, err)
		}
		pkg, err := aspectResponseToPackage(resp, pwd)
		if err != nil {
			// FIXME should be an Errors field entry, right?
			return nil, fmt.Errorf("unable to turn the bazel aspect output into a go/package.Package: %s", err)
		}
		pkgs[pkg.ID] = pkg
		for _, r := range resp.Roots {
			roots[r] = true
		}
		for _, pkg := range pkg.Imports {
			pkgs[pkg.ID] = pkg
		}
	}
	for _, patt := range stdlibPatterns {
		if patt == "builtin" {
			// FIXME this doesn't need to exist when we actually build stdlib support
			bpkg, err := buildBuiltinPackage()
			if err != nil {
				return nil, fmt.Errorf("unable to return query information for builtin package: %w", err)
			}
			roots[bpkg.ID] = true
			pkgs[bpkg.ID] = bpkg
		} else {
			// FIXME doesn't handle main, obvs, but this is just a bootstrap to see how far
			// we can get gopls
			ind := strings.LastIndex(patt, "/")
			if ind == -1 {
				ind = 0
			}
			name := patt[ind:]
			label := fmt.Sprintf(stdlibmaps.StdlibBazelLabelFormat, patt)
			roots[label] = true
			pkgs[label] = &packages.Package{
				ID:      label,
				Name:    name,
				PkgPath: patt,
			}
		}
	}
	sortedPkgs := make([]*packages.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		sortedPkgs = append(sortedPkgs, pkg)
	}
	sort.Slice(sortedPkgs, func(i, j int) bool {
		return sortedPkgs[i].ID < sortedPkgs[j].ID
	})

	sortedRoots := make([]string, 0, len(roots))
	for root := range roots {
		sortedRoots = append(sortedRoots, root)
	}

	sort.Strings(sortedRoots)
	return &driverResponse{
		Sizes:    nil, // FIXME
		Roots:    sortedRoots,
		Packages: sortedPkgs,
	}, nil
}

type aspectResponse struct {
	ID              string   `json:"id"` // the full bazel label for the target
	Name            string   `json:"name"`
	PkgPath         string   `json:"pkg_path"`
	GoFiles         []string `json:"go_files"`          // relative file paths
	CompiledGoFiles []string `json:"compiled_go_files"` // relative file paths
	OtherFiles      []string `json:"other_files"`       // relative file paths
	ExportFile      string   `json:"export_file"`       // relative file path

	Imports map[string]*aspectResponse
	// Usually, just the Go import path of the package.
	Roots []string `json:"roots"`
}

func parseAspectResponse(fp string) (*aspectResponse, error) {
	resp := &aspectResponse{}
	bs, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bs, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func aspectResponseToPackage(resp *aspectResponse, pwd string) (*packages.Package, error) {
	// FIXME check all the places that gopls's golist driver (golist.go, etc.)
	// plops stuff into the Errors struct.
	imports := make(map[string]*packages.Package, len(resp.Imports))
	for pkgpath, pkg := range resp.Imports {
		subpkg, err := aspectResponseToPackage(pkg, pwd)
		if err != nil {
			// FIXME Errors field?
			return nil, fmt.Errorf("unable to turn imported pkg %#v returned by the bazel aspect into a go/packages.Package for returning to go/packages.Load: %s", pkgpath, err)
		}
		imports[pkgpath] = subpkg
	}

	gofiles := absolutizeFilePaths(pwd, resp.GoFiles)
	// builtinImportPaths := extractBuiltinImportPaths(gofiles)
	// for _, imp := range builtinImportPaths {
	// 	_, found := imports[imp]
	// 	if found {
	// 		continue
	// 	}
	// 	bpkg := builtinImportPathToPackage(imp)
	// 	imports[imp] = bpkg
	// }
	return &packages.Package{
		ID:              resp.ID,
		Name:            resp.Name,
		PkgPath:         resp.PkgPath,
		GoFiles:         gofiles,
		CompiledGoFiles: absolutizeFilePaths(pwd, resp.CompiledGoFiles),
		OtherFiles:      absolutizeFilePaths(pwd, resp.OtherFiles),
		ExportFile:      filepath.Join(pwd, resp.ExportFile),
		Imports:         imports,
	}, nil
}

func absolutizeFilePaths(pwd string, fps []string) []string {
	if len(fps) == 0 {
		return fps
	}
	abs := make([]string, len(fps))
	for i, fp := range fps {
		abs[i] = filepath.Join(pwd, fp)
	}
	return abs
}

// FIXME not actually working. this is for gopls.
func buildBuiltinPackage() (*packages.Package, error) {
	id := fmt.Sprintf(stdlibmaps.StdlibBazelLabelFormat, "builtin")
	return &packages.Package{
		ID:      id,
		Name:    "builtin",
		PkgPath: "builtin",
		GoFiles: []string{filepath.Join(pwd, "external/go_sdk/src/builtin/builtin.go")},
		// pkg builtin never has an export file.
		ExportFile: "",
		// pkg builtin never has compiled Go files.
		CompiledGoFiles: nil,
	}, nil
}
