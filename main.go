package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	// "github.com/syndtr/goleveldb/leveldb/util"

	"gopkg.in/vmihailenco/msgpack.v2"
)

const version string = "v0.0.1"

func check(e error) {
	if e != nil {
		panic(e)
	}
}

var interpreters = []string{
	"python",
}

type CommandInfo struct {
	Cmd       string
	CmdKey    string
	Wall      time.Duration
	User      time.Duration
	System    time.Duration
	Start     time.Time
	ExitCode  int
	Resources syscall.Rusage
}

var (
	_ msgpack.CustomEncoder = &CommandInfo{}
	_ msgpack.CustomDecoder = &CommandInfo{}
)

func (c *CommandInfo) EncodeMsgpack(enc *msgpack.Encoder) error {
	return enc.Encode(c.Cmd, c.CmdKey, c.Wall, c.User, c.System, c.Start, c.ExitCode, c.Resources)
}

func (c *CommandInfo) DecodeMsgpack(dec *msgpack.Decoder) error {
	return dec.Decode(&c.Cmd, &c.CmdKey, &c.Wall, &c.User, &c.System, &c.Start, &c.ExitCode, &c.Resources)
}

func printDuration(cmdInfo CommandInfo) {
	fmt.Println(parseDuration(cmdInfo))
}

func parseDuration(cmdInfo CommandInfo) string {
	return fmt.Sprintf("\t%.2f real\t%.2f user\t%.2f sys", cmdInfo.Wall.Seconds(), cmdInfo.User.Seconds(), cmdInfo.System.Seconds())
}

func makeCmdKey(args []string) string {
	return strings.Join(args, " ")
	// cmdKey := args[0]
	// for _, interp := range interpreters {
	// 	if strings.HasPrefix(args[0], interp) {
	// 		for _, kw := range args[1:] {
	// 			if kw[0] != '-' {
	// 				cmdKey += " "
	// 				cmdKey += kw
	// 				return cmdKey
	// 			}
	// 		}
	// 	}
	// }
	// return cmdKey
}

func makeDbKey(cmdInfo CommandInfo) []byte {
	key := fmt.Sprintf("%v %s", cmdInfo.Start, cmdInfo.CmdKey)
	return []byte(key)
}

func makeDbValue(cmdInfo CommandInfo) ([]byte, error) {
	b, err := msgpack.Marshal(&cmdInfo)
	return b, err
}

func recoverDbKey(b []byte) string {
	return string(b)
}

func recoverDbValue(b []byte) (CommandInfo, error) {
	var c CommandInfo
	err := msgpack.Unmarshal(b, &c)
	return c, err
}

func storeCmd(dbfile string, cmdInfo CommandInfo) (err error) {
	db, err := leveldb.OpenFile(dbfile, nil)
	if err != nil {
		return
	}
	defer db.Close()

	byteKey := makeDbKey(cmdInfo)
	byteValue, err := makeDbValue(cmdInfo)
	if err != nil {
		return
	}
	err = db.Put(byteKey, byteValue, nil)

	return
}

func printDb(dbfile string) (err error) {
	db, err := leveldb.OpenFile(dbfile, nil)
	if err != nil {
		return
	}
	defer db.Close()

	iter := db.NewIterator(nil, nil)
	for iter.Next() {
		key := recoverDbKey(iter.Key())
		cmdInfo, err := recoverDbValue(iter.Value())
		_ = err
		fmt.Printf("%s\t= %s\n", key, parseDuration(cmdInfo))
	}
	iter.Release()
	err = iter.Error()

	return
}

func run(args []string) (CommandInfo, error) {
	cmdInfo := CommandInfo{}
	cmdInfo.CmdKey = makeCmdKey(args)
	cmdInfo.Cmd = strings.Join(args, " ")
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
	cmdInfo.Start = start.UTC()
	if exiterr, ok := err.(*exec.ExitError); ok {
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			cmdInfo.ExitCode = status.ExitStatus()
		}
	}
	return cmdInfo, err
}

func main() {
	user, err := user.Current()
	check(err)

	defaultDb := path.Join(user.HomeDir, ".timedatabase")

	printVersion := flag.Bool("version", false, "print version and exit")
	verbose := flag.Bool("verbose", false, "print info about timedb")
	dbFile := flag.String("dbfile", defaultDb, "specify which time database to use")
	dump := flag.Bool("dump", false, "print the whole database")
	search := flag.Bool("search", false, "search in the database")
	flag.Parse()

	cmdArgs := flag.Args()
	dbfile := *dbFile

	if *printVersion {
		fmt.Printf("%s\n", version)
		return
	}

	if *verbose {
		fmt.Printf("version = %s\n", version)
		fmt.Printf("dbfile = %s\n", dbfile)
	}

	if *dump {
		printDb(dbfile)
		return
	}
	if *search {
		printDb(dbfile)
		return
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

	err = storeCmd(dbfile, cmdInfo)
	check(err)

	// byteKey := database.makeDbKey(cmdInfo)
	// byteValue := database.makeDbValue(cmdInfo)

	// check(err)
	// fmt.Println(cmdInfo)
	// fmt.Println(c2)
}
