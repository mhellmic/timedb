package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const version string = "v0.0.1"

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type Duration struct {
	Wall   time.Duration
	User   time.Duration
	System time.Duration
}

type CommandInfo struct {
	ExitCode  int
	Resources interface{}
}

func printDuration(duration Duration) {
	fmt.Printf("\t%.2f real\t%.2f user\t%.2f sys\n", duration.Wall.Seconds(), duration.User.Seconds(), duration.System.Seconds())
}

func run(args []string) (Duration, CommandInfo, error) {
	duration := Duration{}
	cmdInfo := CommandInfo{}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	start := time.Now()
	err := cmd.Start()
	check(err)
	err = cmd.Wait()
	duration.Wall = time.Since(start)
	duration.User = cmd.ProcessState.UserTime()
	duration.System = cmd.ProcessState.SystemTime()
	cmdInfo.Resources = cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if exiterr, ok := err.(*exec.ExitError); ok {
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			cmdInfo.ExitCode = status.ExitStatus()
		}
	}
	return duration, cmdInfo, err
}

func main() {
	printVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	cmdArgs := flag.Args()

	if *printVersion {
		fmt.Printf("%s\n", version)
	}

	if len(cmdArgs) == 0 {
		fmt.Println("No command to measure given. Exiting ...")
		return
	}
	duration, cmdInfo, err := run(cmdArgs)
	if err != nil {
		fmt.Println(err)
	}
	printDuration(duration)
	_ = cmdInfo
}
