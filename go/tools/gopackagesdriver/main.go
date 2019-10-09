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
	"bytes"
	"flag"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
)

var bazelBinDirFlag = flag.String("bazelBinDir", "bazel-bin/external/io_bazel_rules_go/", "the directory to find the go/tools/gopackagesdriver-nonshim/querytool.sh script in")

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("gopackagesdriver: expected to be called with a go/packages pattern to query")
	}
	bazelBinDir := *bazelBinDirFlag
	if envBin := os.Getenv("BAZEL_BIN_DIR"); envBin != "" {
		bazelBinDir = envBin
	}
	environ := os.Environ()
	f, err := os.OpenFile("/Users/jeffhodges/Desktop/wut.txt", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0664)
	if err != nil {
		log.Fatalf("unable to open wut.txt: %s", err)
	}
	log.SetOutput(f)
	dropBazelTestEnv := os.Getenv("BAZEL_DROP_TEST_ENV") == "1"
	log.Println("FIXME BAZEL_DROP_TEST_ENV:", dropBazelTestEnv)

	for _, e := range environ {
		if dropBazelTestEnv && (strings.HasPrefix(e, "TEST_") || strings.HasPrefix(e, "RUNFILES_")) {
			continue
		}
		environ = append(environ, e)
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
	buildCmd.Env = environ
	if err := buildCmd.Run(); err != nil {
		log.Fatalf("gopackagesdriver: error building gopackagesdriver bazel tooling: %v", err)
	}

	// Set up BAZEL_EXEC_ROOT for querytool.sh which is using go_env_cmd to maintain
	// a consistent go envrionment. See the docs on go_env_cmd for why we set up
	// BAZEL_EXEC_ROOT
	execRoot, err := bazelInfoExecRoot(environ)
	if err != nil {
		log.Fatalf("gopackagesdriver: unable to query bazel for the execution root path we need to find the Go files of your bazel packages: %s", err)
	}
	log.Println("FIXME BAZEL_EXEC_ROOT:", execRoot)

	environ = append(environ, "BAZEL_EXEC_ROOT="+execRoot)
	// We have to do this seperate command instead of just calling it with
	// `bazel run` because that will change the PWD to inside the bazel output
	// directory (generally, that's "./bazel-out").  FIXME an alternative I've
	// not tried is passing down down PWD as an argument and using it as the
	// argument to a cd call inside querytool.sh
	cmd := exec.Command(
		path.Join(bazelBinDir, "/go/tools/gopackagesdriver-nonshim/querytool.sh"),
		os.Args[1:]...,
	)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = environ
	if err := cmd.Run(); err != nil {
		log.Fatalf("gopackagesdriver: error running gopackagesdriver bazel tooling: %v", err)
	}

}

func bazelInfoExecRoot(environ []string) (string, error) {
	b, err := bazelOutput(environ, "info", "execution_root")
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(b)), nil
}

func bazelOutput(environ []string, args ...string) ([]byte, error) {
	cmd := exec.Command("bazel")
	cmd.Args = append(cmd.Args, args...)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = environ
	err := cmd.Run()
	if eErr, ok := err.(*exec.ExitError); ok {
		eErr.Stderr = stderr.Bytes()
		err = &StderrExitError{Err: eErr}
	} else {
		// FIXME just always use os.Stderr?
		os.Stderr.Write(stderr.Bytes())
	}
	if err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
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
