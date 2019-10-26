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
	"go/parser"
	"go/token"
	"go/types"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bazelbuild/rules_go/go/tools/gopackagesdriver-nonshim/stdlibmaps"
	"golang.org/x/tools/go/packages"
)

// FIXME package packageLabel and packagePath types and use them.

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
	// f, err := os.OpenFile("/Users/jmhodges/Desktop/wut.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	// if err != nil {
	// 	log.Fatalf("couldn't open log file: %s", err)
	// }
	// defer f.Close()
	// log.SetOutput(f)
	// log.Println("FIXME main 001: targets", os.Args)
	// log.SetPrefix("gopackagesdriver: ")
	// log.SetFlags(0)
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
	execRoot string
)

func run(args []string) error {
	execRoot = os.Getenv("BAZEL_EXEC_ROOT")
	if execRoot == "" {
		return fmt.Errorf("gopackagesdriver: environment BAZEL_EXEC_ROOT must be set for commands to work correctly")
	}

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
	goarch := strings.TrimSpace(os.Getenv("GOARCH"))
	if goarch == "" {
		return fmt.Errorf("GOARCH env var not set but needed to gather size info")
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
		resp, err = packagesFromPatterns(req, targets)
		if err != nil {
			return err
		}
	}

	// SizesFor always return StdSizesm,
	resp.Sizes = types.SizesFor("gc", goarch).(*types.StdSizes)
	log.Println("FIXME run 080", *resp)
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
	fp = filepath.Clean(fp)
	if filepath.IsAbs(fp) {
		pwd := filepath.Clean(os.Getenv("PWD"))
		if !strings.HasPrefix(fp, pwd) {
			return "", fmt.Errorf("error converting filepath %#v to bazel file label: filepath is absolute but the file doesn't exist in the tree below the current working directory (%#v)", fp, pwd)
		}
		if pwd != "/" {
			pwd = pwd + "/"
		}
		fp = strings.TrimPrefix(fp, pwd)
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
	// FIXME use label_kind to prefer go_library over go_binary
	packageSplat := label[:ind+1] + "*"
	q := fmt.Sprintf("attr(\"srcs\", %s, %s) intersect (kind(go_library, %s) union kind(go_binary, %s))", label, packageSplat, packageSplat, packageSplat)
	bs, err := bazelQuery(q)
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

const goPkgsDriverOutputGroup = "gopackagesdriver_data"

// FIXME only really supports one target
func packagesFromPatterns(req *driverRequest, targets []string) (*driverResponse, error) {
	log.Println("FIXME packagesFromPatterns 001: targets", targets)
	bazelTargets := make([]packageID, 0, len(targets))
	var stdlibPatterns []string
	for _, targ := range targets {
		if imp, ok := stdlibmaps.StdlibBazelLabelToImportPath[targ]; ok {
			stdlibPatterns = append(stdlibPatterns, imp)
		} else if _, ok := stdlibmaps.StdlibImportPathToBazelLabel[targ]; ok {
			stdlibPatterns = append(stdlibPatterns, targ)
		} else {
			bazelTargets = append(bazelTargets, packageID(targ))
		}
	}

	// FIXME handle len(targets) == 0 explicilty. right now it's just a bunch of
	// code to move around, so I'm skipping it since it's just a warning from
	// bazel.
	log.Println("FIXME packagesFromPatterns 010 bazelTargets:", bazelTargets, "stdlibPatterns:", stdlibPatterns)
	pkgs := make(map[packageID]*packages.Package)
	roots := make(map[packageID]bool)

	if len(bazelTargets) != 0 {
		err := packagesFromBazelTargets(req.Mode, req.BuildFlags, bazelTargets, pkgs, roots)
		if err != nil {
			// FIXME If we do the Errors field work, this might need more
			// context because it'll only happen in rare, serious cases.
			return nil, err
		}
	}
	if len(stdlibPatterns) != 0 {
		err := packagesFromStdlibPatterns(req, stdlibPatterns, pkgs, roots)
		if err != nil {
			// FIXME If we do the Errors field work, this might need more
			// context because it'll only happen in rare, serious cases.
			return nil, err
		}
	}

	log.Println("FIXME packagesFromPatterns 020", pkgs)
	log.Println("FIXME packagesFromPatterns 021", roots)
	sortedPkgs := make([]*packages.Package, 0, len(pkgs))
	allPkgs := make(map[packageID]bool)
	for _, pkg := range pkgs {
		id := packageID(pkg.ID)
		if !allPkgs[id] {
			sortedPkgs = append(sortedPkgs, pkg)
			allPkgs[id] = true
		}
	}
	// We use a separate loop to make sure we use root (and likely "full")
	// versions of packages instead of versions of Packages that could be
	// slimmer (like a NeedsImport pkg with just ID set)
	// FIXME this may actually
	// not be necessary if the NeedsImport pkgs in Imports (etc.) use the root
	// versions
	log.Println("FIXME packagesFromPatterns 060", pkgs)
	for _, pkg := range pkgs {
		for _, ipkg := range pkg.Imports {
			id := packageID(ipkg.ID)
			if !allPkgs[id] {
				sortedPkgs = append(sortedPkgs, ipkg)
				allPkgs[id] = true
			}
		}
	}
	sort.Slice(sortedPkgs, func(i, j int) bool {
		return sortedPkgs[i].ID < sortedPkgs[j].ID
	})

	sortedRoots := make([]string, 0, len(roots))
	for root := range roots {
		sortedRoots = append(sortedRoots, string(root))
	}

	for _, p := range sortedPkgs {
		log.Println("FIXME packagesFromPatterns 097:", p.ID, "Imports: ", p.Imports)
	}
	log.Println("FIXME packagesFromPatterns 098 sortedPkgs:", sortedPkgs)
	log.Println("FIXME packagesFromPatterns 099 sortedRoots:", sortedRoots)

	sort.Strings(sortedRoots)
	return &driverResponse{
		Sizes:    nil, // FIXME
		Roots:    sortedRoots,
		Packages: sortedPkgs,
	}, nil
}

// packageID is used to distinguish Package.ID from Package.PkgPath here. It's a
// bazel label and maybe we should call it "packageLabel", instead?
type packageID string

func packagesFromBazelTargets(mode packages.LoadMode, buildFlags []string, bazelTargets []packageID, pkgs map[packageID]*packages.Package, roots map[packageID]bool) error {
	pbuf, err := bazelBuildAspects(mode, buildFlags, bazelTargets)
	if err != nil {
		return err
	}

	files, err := extractBazelAspectOutputFilePaths(pbuf)
	if err != nil {
		return err
	}

	log.Println("FIXME packagesFromBazelTargets 45:", mode, files)
	for fp, _ := range files {
		resp, err := parseAspectResponse(fp)
		if err != nil {
			return fmt.Errorf("unable to parse JSON response in file %#v in returened aspect: %s", fp, err)
		}
		_, found := pkgs[packageID(resp.ID)]
		if found {
			continue
		}
		pkg := aspectResponseToPackage(resp, execRoot)
		log.Println("FIXME packagesFromBazelTargets 60", pkg.GoFiles)

		if (mode & packages.NeedDeps) != 0 {
			err = aspectResponseAddFullPackagesToImports(resp, pkg, pkgs)
			if err != nil {
				return err
			}
		} else if (mode & packages.NeedImports) != 0 {
			err = aspectResponseAddIDOnlyPackagesToImports(resp, pkg)
			if err != nil {
				return err
			}
		}
		pkgs[packageID(pkg.ID)] = pkg
		roots[packageID(pkg.ID)] = true
	}
	return nil
}

func packagesFromStdlibPatterns(req *driverRequest, stdlibPatterns []string, pkgs map[packageID]*packages.Package, roots map[packageID]bool) error {
	for _, patt := range stdlibPatterns {
		_, found := pkgs[packageID(stdlibmaps.StdlibImportPathToBazelLabel[patt])]
		if found {
			continue
		}
		spkg, err := buildStdlibPackageFromImportPath(patt)
		if err != nil {
			// FIXME Errors field?
			return err
		}

		// FIXME use a cache with the other calls to them (but not the top-level
		// pkgs cache which could hold different info?)
		if (req.Mode & packages.NeedDeps) != 0 {
			err = addFullPackagesToImports(nil, spkg, pkgs)
			if err != nil {
				return err
			}
		} else if (req.Mode & packages.NeedImports) != 0 {
			err = addIDOnlyPackagesToImports(nil, spkg)
			if err != nil {
				return err
			}
		}

		pkgs[packageID(spkg.ID)] = spkg
		roots[packageID(spkg.ID)] = true
	}
	return nil
}

type aspectResponse struct {
	ID              string   `json:"id"` // the full bazel label for the target
	Name            string   `json:"name"`
	PkgPath         string   `json:"pkg_path"`
	GoFiles         []string `json:"go_files"`          // relative file paths
	CompiledGoFiles []string `json:"compiled_go_files"` // relative file paths
	OtherFiles      []string `json:"other_files"`       // relative file paths
	ExportFile      string   `json:"export_file"`       // relative file path

	// DepsImportPaths is the Go import paths of the bazel targets listed in the
	// deps field of the target queried for to their label.
	DepImportPathsToLabels map[string]string `json:"dep_importpaths_to_labels"`

	// Imports is the aspectResponses of the packages that are bazel
	// dependencies of this package. Does not, currently, include standard
	// libraries and those have to be parsed separately.
	Imports map[packageID]*aspectResponse `json:"imports"`
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

func aspectResponseAddFullPackagesToImports(resp *aspectResponse, pkg *packages.Package, pkgs map[packageID]*packages.Package) error {
	return addFullPackagesToImports(resp.Imports, pkg, pkgs)
}

func addFullPackagesToImports(imports map[packageID]*aspectResponse, opkg *packages.Package, pkgs map[packageID]*packages.Package) error {
	log.Println("FIXME addFullPackagesToImports 01 opkg.ID:", opkg.ID, "GoFiles count:", len(opkg.GoFiles))
	// FIXME Refactor this entire func with the main parseAspectResponse loop
	for _, fp := range opkg.GoFiles {
		// FIXME all errors in here should probably be Errors field entries?
		// FIXME should return packageIDs?
		labels, err := findStdlibImportPathsToBazelLabelsInFile(fp)
		if err != nil {
			return fmt.Errorf("error while trying to parse %#v for imports of standard libs: %s", fp, err)
		}

		for imp, label := range labels {
			if _, found := opkg.Imports[imp]; found {
				continue
			}
			spkg, found := pkgs[label]
			if !found {
				var err error
				spkg, err = buildStdlibPackageFromImportPath(imp)
				if err != nil {
					return err
				}
				pkgs[label] = spkg
			}
			opkg.Imports[imp] = spkg
		}
	}
	log.Println("FIXME addFullPackagesToImports 50 labelsToJSONFiles:", imports, "opkg.Imports:", opkg.Imports)
	for id, iresp := range imports {
		ipkg, found := pkgs[id]
		if !found {
			// FIXME add as package.Errors
			ipkg = aspectResponseToPackage(iresp, execRoot)
			err := aspectResponseAddFullPackagesToImports(iresp, ipkg, pkgs)
			if err != nil {
				return err
			}
			pkgs[id] = ipkg
		}
		opkg.Imports[ipkg.PkgPath] = ipkg
	}
	return nil
}

// FIXME remove returned error and make it an Errors field thing
func aspectResponseAddIDOnlyPackagesToImports(resp *aspectResponse, pkg *packages.Package) error {
	return addIDOnlyPackagesToImports(resp.DepImportPathsToLabels, pkg)
}

func addIDOnlyPackagesToImports(depImpToLabels map[string]string, pkg *packages.Package) error {
	log.Println("FIXME aspectResponseAddIDOnlyPackagesToImports 1", pkg.GoFiles)
	// FIXME no longer needed since we always set a non-nil Imports now?
	if pkg.Imports == nil {
		pkg.Imports = make(map[string]*packages.Package)
	}
	for _, fp := range pkg.GoFiles {
		// FIXME all errors in here should probably be Errors field entries?
		labels, err := findStdlibImportPathsToBazelLabelsInFile(fp)
		if err != nil {
			return fmt.Errorf("error while trying to parse %#v for imports of standard libs: %s", fp, err)
		}

		for imp, label := range labels {
			if _, found := pkg.Imports[imp]; found {
				continue
			}
			pkg.Imports[imp] = &packages.Package{ID: string(label)}
		}
	}
	for imp, label := range depImpToLabels {
		pkg.Imports[imp] = &packages.Package{ID: label}
	}
	log.Println("FIXME aspectResponseAddIDOnlyPackagesToImports 100", pkg.GoFiles, pkg.Imports)
	return nil
}

func aspectResponseToPackage(resp *aspectResponse, pwd string) *packages.Package {
	// FIXME check all the places that gopls's golist driver (golist.go, etc.)
	// plops stuff into the Errors struct.
	log.Println("FIXME aspectResponseToPackage 1", resp.GoFiles)
	return &packages.Package{
		ID:              resp.ID,
		Name:            resp.Name,
		PkgPath:         resp.PkgPath,
		GoFiles:         absolutizeFilePaths(pwd, resp.GoFiles),
		CompiledGoFiles: absolutizeFilePaths(pwd, resp.CompiledGoFiles),
		OtherFiles:      absolutizeFilePaths(pwd, resp.OtherFiles),
		ExportFile:      filepath.Join(pwd, resp.ExportFile),
		Imports:         make(map[string]*packages.Package),
	}
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

var stdlibExportPrefix = filepath.Join(os.Getenv("GOROOT"), "pkg", os.Getenv("GOOS")+"_"+os.Getenv("GOARCH"))

// FIXME not actually working. this is for gopls.
func buildStdlibPackageFromImportPath(imp string) (*packages.Package, error) {
	// FIXME do this out for real (with the go list driver?)
	ind := strings.LastIndex(imp, "/")
	if ind == -1 {
		ind = 0
	} else {
		ind++
	}
	name := imp[ind:]
	dir := filepath.Join(os.Getenv("GOROOT"), "src", imp)
	fis, err := ioutil.ReadDir(dir)
	// FIXME tag on to Errors, instead
	if err != nil {
		return nil, err
	}

	var goFiles []string
	var compiledGoFiles []string
	for _, fi := range fis {
		full := filepath.Join(dir, fi.Name())
		if strings.HasSuffix(fi.Name(), "_test.go") {
			// do nothing
		} else if strings.HasSuffix(fi.Name(), ".go") {
			goFiles = append(goFiles, full)
			// FIXME this is probably wrong
			compiledGoFiles = append(compiledGoFiles, full)
		}
	}

	label := stdlibBazelLabel(imp)
	// FIXME add Export
	return &packages.Package{
		ID:              label,
		Name:            name,
		PkgPath:         imp,
		GoFiles:         goFiles,
		CompiledGoFiles: compiledGoFiles,
		// FIXME better way to generate this?
		ExportFile: filepath.Join(stdlibExportPrefix, imp),
	}, nil
}

func findStdlibImportPathsToBazelLabelsInFile(fp string) (map[string]packageID, error) {
	fset := token.NewFileSet()

	// FIXME apply build tags here.
	f, err := parser.ParseFile(fset, fp, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	impToLabels := make(map[string]packageID)
	log.Println("FIXME findStdlibImportPathsToBazelLabelsInFile 1", fp, ", Imports:", f.Imports)
	for _, impSpec := range f.Imports {
		imp := strings.Trim(impSpec.Path.Value, `"`)
		log.Println("FIXME findStdlibBazelLabelsInFile 20", imp)
		if label, found := stdlibmaps.StdlibImportPathToBazelLabel[imp]; found {
			impToLabels[imp] = packageID(label)
		}
	}
	log.Println("FIXME findStdlibImportPathsToBazelLabelsInFile 100", impToLabels)
	return impToLabels, nil
}

func stdlibBazelLabel(importPath string) string {
	return fmt.Sprintf(stdlibmaps.StdlibBazelLabelFormat, importPath)
}
