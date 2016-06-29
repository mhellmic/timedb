package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
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
	duration := Duration{}
	cmdInfo := CommandInfo{}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	start := time.Now()
	err := cmd.Run()
	duration.Wall = time.Since(start)
	duration.User = cmd.ProcessState.UserTime()
	duration.System = cmd.ProcessState.SystemTime()
	// GetUserSystemTimes(&duration)
	if err != nil {
		fmt.Println(err)
	}
	cmdInfo.ExitCode = 0
	cmdInfo.Resources = cmd.ProcessState.SysUsage()
	fmt.Printf("\t%v real\t%v user\t%v sys\n", duration.Wall.Seconds(), duration.User.Seconds(), duration.System.Seconds())
	fmt.Println(cmdInfo.Resources)
}
