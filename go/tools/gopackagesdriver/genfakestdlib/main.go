package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io/ioutil"
	"log"
	"os"
	"text/template"
)

var (
	pkgList = flag.String("pkglist", "", "the file path to packages_list (required)")
	pkgName = flag.String("pkgname", "main", "the name to use in the 'package' statement in the generated file")
)

type tmplData struct {
	GenPkgName string
	Pkgs       []pkg
}

type pkg struct {
	StdPkgImport     string
	StdPkgBazelLabel string
}

const stdlibLabelFmt = "@go_sdk//:stdlib-%s"

func main() {
	// FIXME using this style means we either have to check the out of this in
	// to the repo to allow `go get` to work, or it requires bazel. But that
	// latter idea might lead to people using the
	// @io_bazel_rules_go//go/tools/gopackagesdriver in their own workspace
	// which would be difficult to configure across multiple projects.

	// FIXME actually the smart thing to do would be to make gopackagesdriver
	// go-gettable by making it a very thing wrapper over a `bazel run` call of
	// binary in @io_bazel_rules_go, yeah?
	flag.Parse()
	if *pkgList == "" {
		log.Fatalf("genfakestdlib: required `-pkgList` argument not provided")
	}
	b, err := ioutil.ReadFile(*pkgList)
	if err != nil {
		log.Fatalf("genfakestdlib: error opening %#v: %s", *pkgList, err)
	}
	lines := bytes.Split(b, []byte{'\n'})
	pkgs := make([]pkg, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		pkgs = append(pkgs, pkg{
			StdPkgImport:     string(line),
			StdPkgBazelLabel: fmt.Sprintf(stdlibLabelFmt, line),
		})
	}
	buf := &bytes.Buffer{}
	err = mapTmpl.Execute(buf, tmplData{
		GenPkgName: *pkgName,
		Pkgs:       pkgs,
	})
	if err != nil {
		log.Fatalf("genfakestdlib: unable to execute the templated file to generate the Go code: %s", err)
	}
	code, err := format.Source(buf.Bytes())
	if err != nil {
		log.Fatalf("genfakestdlib: unable to format generated Go code: %s", err)
	}
	os.Stdout.Write(code)
}

var mapTmpl = template.Must(template.New("maps").Parse(`package {{.GenPkgName}}

var stdlibByImportPath = map[string]string{
	{{ range $i, $pkg := .Pkgs -}}
	{{ $pkg.StdPkgImport | printf "%#v" }}: {{ $pkg.StdPkgBazelLabel | printf "%#v" }},
	{{ end }}
}

var stdlibByLabel = map[string]string{
	{{ range $i, $pkg := .Pkgs -}}
	{{ $pkg.StdPkgBazelLabel | printf "%#v" }}: {{ $pkg.StdPkgImport | printf "%#v" }},
	{{ end }}
}
`))
