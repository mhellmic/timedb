package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"gopkg.in/vmihailenco/msgpack.v2"
)

const version string = "v0.0.1"

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type CommandInfo struct {
	Wall      time.Duration
	User      time.Duration
	System    time.Duration
	ExitCode  int
	Resources syscall.Rusage
}

var (
	_ msgpack.CustomEncoder = &CommandInfo{}
	_ msgpack.CustomDecoder = &CommandInfo{}
)

func (c *CommandInfo) EncodeMsgpack(enc *msgpack.Encoder) error {
	return enc.Encode(c.Wall, c.User, c.System, c.ExitCode, c.Resources)
}

func (c *CommandInfo) DecodeMsgpack(dec *msgpack.Decoder) error {
	return dec.Decode(&c.Wall, &c.User, &c.System, &c.ExitCode, &c.Resources)
}

func printDuration(cmdInfo CommandInfo) {
	fmt.Printf("\t%.2f real\t%.2f user\t%.2f sys\n", cmdInfo.Wall.Seconds(), cmdInfo.User.Seconds(), cmdInfo.System.Seconds())
}

func run(args []string) (CommandInfo, error) {
	cmdInfo := CommandInfo{}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	start := time.Now()
	err := cmd.Start()
	check(err)
	err = cmd.Wait()
	cmdInfo.Wall = time.Since(start)
	cmdInfo.User = cmd.ProcessState.UserTime()
	cmdInfo.System = cmd.ProcessState.SystemTime()
	cmdInfo.Resources = *(cmd.ProcessState.SysUsage().(*syscall.Rusage))
	if exiterr, ok := err.(*exec.ExitError); ok {
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			cmdInfo.ExitCode = status.ExitStatus()
		}
	}
	return cmdInfo, err
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
	cmdInfo, err := run(cmdArgs)
	if err != nil {
		fmt.Println(err)
	}
	printDuration(cmdInfo)

	b2, err := msgpack.Marshal(&cmdInfo)
	check(err)
	var c2 CommandInfo
	err = msgpack.Unmarshal(b2, &c2)
	check(err)
	fmt.Println(cmdInfo)
	fmt.Println(c2)
}
