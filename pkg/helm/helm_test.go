package helm

import (
	"os"
	"os/exec"
	"testing"
)

func red(msg string) string {
	return "\033[31m" + msg + "\033[39m\n\n"
}

func helperCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func Test_HelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// check arguments perhaps?
	//fmt.Printf(os.Stdout, "asfd")
	os.Exit(0)
}

func Test_Exists(t *testing.T) {
	/*execCommand = helperCommand
	defer func() { execCommand = exec.Command }()

	out, err := Exists("test")
	if err != nil {
		t.Errorf("Expected nil error, got %#v", err)
	}
	if out != true {
		t.Errorf("Expected %q, got %q", true, out)
	}*/
}
