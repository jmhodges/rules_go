// gopackagesdriver provides the interface between go/packages (and consumers of
// go/packages like gopls) and bazel's rules_go code to provide cross-editor
// IDE-style support. It, like other bazel tooling, expects to be run in the
// top-level directory of a bazel project.
//
// The path to its binary is meant to be set to the
// $GOPACKAGESDRIVER environment variable or installed as gopackagesdriver in a
// user's path so that go/packages can use it.
//
package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"path"
)

var bazelBinDir = flag.String("bazelBinDir", "bazel-bin", "the directory to find the go/tools/gopackagesdriver-nonshim/querytool.sh script in")

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("gopackagesdriver: expected to be called with a go/packages pattern to query")
	}
	// We call out to gopackagesdriver-nonshim so that it can poke around in
	// GOROOT, to keep gopackagesdriver and the stdlib package list up to date
	// with the actual Go and rules_go versions the user actually has, and to
	// allow for us to generate the Go protobuf code without having to check it
	// in, while allowing gopackagesdriver itself to be installed with the
	// normal go build tooling.
	//
	// You might notice that gopackagesdriver-nonshim also has to call out to
	// bazel and ask if we couldn't do the work in gopackagesdriver-nonshim in
	// go binaries called from the bazel aspects that gopackagesdriver-nonshim
	// uses.  We, unfortunately, cannot because part of the problem
	// gopackagesdriver-nonshim has to solve is turning Go import paths into
	// bazel labels. Those import paths will include stdlib import paths that
	// rules_go does not actaully have go_library targets. Intercepting requests
	// for the stdlib and returning and responding to the fake bazel targets
	// gopackagesdriver-nonshim creates requires the go_context set up for
	// gopackagesdriver-nonshim's querytool.
	//
	// Along the way in there, it's nice to just shortcut the stdlib
	// packages.Package generation by doing it in gopackagesdriver-nonshim.
	buildCmd := exec.Command("bazel", "build", "@io_bazel_rules_go//go/tools/gopackagesdriver-nonshim:querytool")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		log.Fatalf("gopackagesdriver: error building gopackagesdriver bazel tooling: %v", err)
	}

	// We have to do this seperate command instead of just calling it with
	// `bazel run` because that will change the PWD to inside the bazel output
	// directory (generally, that's "./bazel-out").  FIXME an alternative I've
	// not tried is passing down down PWD as an argument and using it as the
	// argument to a cd call inside querytool.sh
	cmd := exec.Command(
		path.Join(*bazelBinDir, "/go/tools/gopackagesdriver-nonshim/querytool.sh"),
		os.Args[1:]...,
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("gopackagesdriver: error running gopackagesdriver bazel tooling: %v", err)
	}

}
