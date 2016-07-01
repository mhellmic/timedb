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
	"github.com/syndtr/goleveldb/leveldb/util"

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

type Config struct {
	Verbose bool
	Dbfile  string
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
	key := fmt.Sprintf("%v %s", cmdInfo.Start.Format(dbTimeFormat), cmdInfo.CmdKey)
	return []byte(key)
}

func makeDbValue(cmdInfo CommandInfo) ([]byte, error) {
	b, err := msgpack.Marshal(&cmdInfo)
	return b, err
}

const dbTimeFormat = "2006-01-02 15:04:05.999"
const dbLenTime = len(dbTimeFormat)

func recoverDbKey(b []byte) (time.Time, string) {
	keyString := string(b)
	cmdIdx := dbLenTime + 1
	// it seems impossible to force trailing zeros in the time storage format.
	// the shortest should be "2006-01-02 15:04:05.0", though
	t, err := time.Parse(dbTimeFormat, keyString[:dbLenTime])
	if err != nil {
		t, err = time.Parse(dbTimeFormat, keyString[:dbLenTime-1])
		cmdIdx = dbLenTime
	}
	if err != nil {
		t, err = time.Parse(dbTimeFormat, keyString[:dbLenTime-2])
		cmdIdx = dbLenTime - 1
	}
	check(err)
	cmd := keyString[cmdIdx:]
	return t, cmd
}

func recoverDbValue(b []byte) (CommandInfo, error) {
	var c CommandInfo
	err := msgpack.Unmarshal(b, &c)
	return c, err
}

func storeCmd(config Config, cmdInfo CommandInfo) (err error) {
	db, err := leveldb.OpenFile(config.Dbfile, nil)
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

func printDb(config Config) (err error) {
	db, err := leveldb.OpenFile(config.Dbfile, nil)
	if err != nil {
		return
	}
	defer db.Close()

	iter := db.NewIterator(nil, nil)
	for iter.Next() {
		start, cmd := recoverDbKey(iter.Key())
		_, _ = start, cmd
		cmdInfo, err := recoverDbValue(iter.Value())
		_ = err
		fmt.Printf("%s\t%s\t= %s\n", start.In(time.Local).Format("2006-01-02 15:04:05"),
			cmd, parseDuration(cmdInfo))
	}
	iter.Release()
	err = iter.Error()

	return
}

func parseTime(arg string) (time.Time, error) {
	var err error
	var t time.Time
	var formats = []string{
		"2.1.2006_15:04",
		"2.1.2006_15",
		"2.1.2006",
		"15:04",
	}
	for _, f := range formats {
		t, err = time.ParseInLocation(f, arg, time.Local)
		if err == nil {
			if t.Year() == 0 {
				now := time.Now()
				t = t.AddDate(now.Year(), int(now.Month())-1, now.Day()-1)
			}
			return t, err
		}
	}
	return time.Time{}, err
}

func parseStartEnd(args []string) (time.Time, time.Time, int) {
	start := time.Time{}
	end := time.Now()
	for i, arg := range args {
		ts := strings.SplitN(arg, "-", 2)
		if len(ts) == 2 {
			end, endErr := parseTime(ts[1])
			start, startErr := parseTime(ts[0])
			if startErr == nil || endErr == nil {
				if endErr != nil {
					end = time.Now()
				}
				return start, end, i
			}
		}
		// a single time is interpreted as single day
		start, startErr := parseTime(arg)
		if startErr == nil {
			end = start.Add(24 * time.Hour)
			return start, end, i
		}
	}
	return start, end, -1
}

func findInCmdKey(cmd string, keywords []string) bool {
	allFound := true
	for _, k := range keywords {
		if found := strings.Contains(cmd, k); !found {
			return false
		}
	}
	return allFound
}

func searchDb(config Config, args []string) (err error) {
	db, err := leveldb.OpenFile(config.Dbfile, nil)
	if err != nil {
		return
	}
	defer db.Close()

	start, end, timeIdx := parseStartEnd(args)
	var keywords []string
	if len(args) == 0 || timeIdx < 0 {
		keywords = args
	} else {
		keywords = args[timeIdx+1:]
	}
	if config.Verbose {
		fmt.Printf("search min: %v\nsearch max: %v\n", start, end)
		fmt.Printf("keywords: %d %v\n", len(keywords), keywords)
	}

	lowerBound := []byte(start.UTC().String())
	upperBound := []byte(end.UTC().String())

	iter := db.NewIterator(&util.Range{Start: lowerBound, Limit: upperBound}, nil)
	for iter.Next() {
		start, cmd := recoverDbKey(iter.Key())
		_ = start
		if findInCmdKey(cmd, keywords) {
			cmdInfo, err := recoverDbValue(iter.Value())
			check(err)
			fmt.Printf("%s\t%s\t= %s\n", start.In(time.Local).Format("2006-01-02 15:04:05"),
				cmd, parseDuration(cmdInfo))
		}
	}
	iter.Release()
	err = iter.Error()

	return
}

func run(config Config, args []string) (CommandInfo, error) {
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

	if config.Verbose {
		fmt.Printf("cmd: %s\nstart time: %v\nduration: %v\n", cmdInfo.Cmd, cmdInfo.Start.Format("2006-01-02 15:04:05.999 -0700 MST"), cmdInfo.Wall)
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

	config := Config{
		Verbose: *verbose,
		Dbfile:  *dbFile,
	}
	cmdArgs := flag.Args()
	// dbfile := *dbFile

	if *printVersion {
		fmt.Printf("%s\n", version)
		return
	}

	if config.Verbose {
		fmt.Printf("version = %s\n", version)
		fmt.Printf("dbfile = %s\n", config.Dbfile)
	}

	if *dump {
		printDb(config)
		return
	}
	if *search {
		searchDb(config, cmdArgs)
		return
	}
	if len(cmdArgs) == 0 {
		fmt.Println("No command to measure given. Exiting ...")
		return
	}
	cmdInfo, err := run(config, cmdArgs)
	if err != nil {
		fmt.Println(err)
	}
	printDuration(cmdInfo)

	err = storeCmd(config, cmdInfo)
	check(err)

	// byteKey := database.makeDbKey(cmdInfo)
	// byteValue := database.makeDbValue(cmdInfo)

	// check(err)
	// fmt.Println(cmdInfo)
	// fmt.Println(c2)
}
