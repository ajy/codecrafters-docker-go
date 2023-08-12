package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

func copyExecutable(srcPath string, destPath string) error {
	sourceFile, err := os.Open(srcPath)
	defer sourceFile.Close()

	if err != nil {
		return err
	}

	destinationDir := filepath.Dir(destPath)
	err = os.MkdirAll(destinationDir, 0777)

	if err != nil {
		return err
	}

	destinationFile, err := os.Create(destPath)
	defer destinationFile.Close()

	if err != nil {
		return err
	}

	_, err = io.Copy(destinationFile, sourceFile)

	if err != nil {
		return err
	}

	err = destinationFile.Chmod(0777)
	if err != nil {
		return err
	}

	return nil
}

func isolatedRun(command string, inputArgs ...string) error {
	dname, mkdirErr := os.MkdirTemp("", "tempDockerRun")
	defer os.RemoveAll(dname)

	if mkdirErr != nil {
		fmt.Println("Error while making dir")
		return mkdirErr
	}

	command_locating_cmd := exec.Command("which", command)
	command_location, command_locating_err := command_locating_cmd.Output()

	if command_locating_err != nil {
		fmt.Println("Error while locating cmd")
		return command_locating_err
	}

	jailed_cmd := path.Join(dname, command)
	errWhileCopyingCommand := copyExecutable(strings.TrimSuffix(string(command_location), "\n"), jailed_cmd)

	if errWhileCopyingCommand != nil {
		fmt.Println("Error while copying executable")
		return errWhileCopyingCommand
	}

	initial_args := [...]string{dname, command}
	args := append(initial_args[:], inputArgs...)
	cmd := exec.Command("chroot", args...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Isolate cmd in it's own process namespace
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID,
	}

	return cmd.Run()

}

// Usage: your_docker.sh run <image> <command> <arg1> <arg2> ...
func main() {
	command := os.Args[3]
	args := os.Args[4:len(os.Args)]

	if err := isolatedRun(command, args...); err != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr):
			fmt.Println("ExitErr: %v", err)
			os.Exit(exitErr.ProcessState.ExitCode())
		default:
			fmt.Println("Err: %v", err)
			os.Exit(1)
		}
	}
}
