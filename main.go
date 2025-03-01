/*
   timedb -- An alternative to `time` that saves its own history.
   Copyright (C) 2016  Martin Hellmich (mhellmic@gmail.com)

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"

	"gopkg.in/vmihailenco/msgpack.v2"
)

var currentUser *user.User

const version string = "v1.0.0"

const licenseHelp string = `
    timedb  Copyright (C) 2016  Martin Hellmich (mhellmic@gmail.com)
    This program comes with ABSOLUTELY NO WARRANTY.
    This is free software, and you are welcome to redistribute it
    under certain conditions.`

const kwHelpText string = `Normal Keywords:
Find keyword in the command string.
Examples:
	timedb -search python
	timedb -search '-f 48'
	timedb -search /home/user ls

Timerange Keywords:
Find only during the specified time range <start>-<end>.
One of start or end can be omitted, allowed time formats are
	d.m.y, d.m.y_hh:mm, hh:mm (read as 'today' at this time)
If only a single time is given, the search is limited to the
following 24h period
Examples:
	timedb -search 2.1.2006-
	timedb -search 3.12.1998-4.12.1999_12:43
	timedb -search -- -1.1.2008
	timedb -search 10.10.1995

Special Keywords (form <keyword>(<|=|>)value):
Find commands that satisfy certain runtime criteria.
Allowed keywords are:
	Walltime	<duration>		'Walltime>10s'
	Usertime	<duration>		'Usertime<2m10s'
	Systemtime	<duration>
	Exitcode	<number>		'Exitcode=1'
	Signals		<number>

Combined example:
	timedb -search 2.1.2015- python 'Walltime>30s' 'Exitcode=0'`

type ParseError struct {
	toParse string
	as      string
}

func (s ParseError) Error() string {
	return fmt.Sprintf("parse: could not parse %s as %s", s.toParse, s.as)
}

// errorString is a trivial implementation of error.
type errorString struct {
	s string
}

func (e *errorString) Error() string {
	return e.s
}

// func check(e error) {
// 	if e != nil {
// 		panic(e)
// 	}
// }

type Config struct {
	Verbose bool
	Dbfile  string
}

type Keyword interface {
	GetName() string
	Matches(interface{}) bool
}

type IntKeyword struct {
	Name     string
	Relation int
	Value    int
}

func (kw IntKeyword) GetName() string {
	return kw.Name
}

func (kw IntKeyword) Matches(arg interface{}) bool {
	val := kw.Value
	comp := arg.(int)
	switch {
	case kw.Relation < 0:
		if comp >= val {
			return false
		}
	case kw.Relation == 0:
		if comp != val {
			return false
		}
	case kw.Relation > 0:
		if comp <= val {
			return false
		}
	}
	return true
}

type DurationKeyword struct {
	Name     string
	Relation int
	Value    time.Duration
}

func (kw DurationKeyword) GetName() string {
	return kw.Name
}

func (kw DurationKeyword) Matches(arg interface{}) bool {
	val := kw.Value
	comp := arg.(time.Duration)
	switch {
	case kw.Relation < 0:
		if comp >= val {
			return false
		}
	case kw.Relation == 0:
		if comp != val {
			return false
		}
	case kw.Relation > 0:
		if comp <= val {
			return false
		}
	}
	return true
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

func recoverDbKey(b []byte) (time.Time, string, error) {
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
	if err != nil {
		return t, "", err
	}
	cmd := keyString[cmdIdx:]
	return t, cmd, nil
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
		start, cmd, err := recoverDbKey(iter.Key())
		if err != nil {
			fmt.Printf("WARNING: %s, skipping entry\n", err)
			continue
		}
		_ = cmd
		cmdInfo, err := recoverDbValue(iter.Value())
		if err != nil {
			fmt.Printf("WARNING: %s, skipping entry\n", err)
			continue
		}
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

func parseStartEnd(args []string) (time.Time, time.Time, error) {
	start := time.Time{}
	end := time.Now()

	if len(args) == 0 {
		return start, end, &errorString{s: "no arguments available"}
	}
	arg := args[0]
	ts := strings.SplitN(arg, "-", 2)
	if len(ts) == 2 {
		end, endErr := parseTime(ts[1])
		start, startErr := parseTime(ts[0])
		if startErr == nil || endErr == nil {
			if endErr != nil {
				end = time.Now()
			}
			return start, end, nil
		}
	}
	// a single time is interpreted as single day
	start, startErr := parseTime(arg)
	if startErr == nil {
		end = start.Add(24 * time.Hour)
		return start, end, nil
	}

	return start, end, startErr
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

func parseKeywordRelation(arg string) (int, error) {
	switch {
	case strings.Contains(arg, "<"):
		return -1, nil
	case strings.Contains(arg, "="):
		return 0, nil
	case strings.Contains(arg, ">"):
		return +1, nil
	}
	return 0, ParseError{toParse: arg, as: "(<|=|>)"}
}

var durationKwNames = []string{
	"Walltime", "Systemtime", "Usertime",
}

var intKwNames = []string{
	"Exitcode", "Signals",
}

func isIn(s string, names []string) bool {
	for _, n := range names {
		if s == n {
			return true
		}
	}
	return false
}

func parseKeyword(arg string) (Keyword, error) {
	var iKw Keyword
	kwRegexp := regexp.MustCompile(">|<|=")
	s := kwRegexp.Split(arg, -1)
	if len(s) == 2 {
		kw := s[0]
		value := s[1]
		switch {
		case isIn(kw, durationKwNames):
			d, err := time.ParseDuration(value)
			if err != nil {
				return iKw, ParseError{toParse: value, as: "time.Duration"}
			}
			r, err := parseKeywordRelation(arg)
			if err != nil {
				return iKw, err
			}
			iKw = DurationKeyword{Name: kw, Relation: r, Value: d}
			return iKw, nil
		case isIn(kw, intKwNames):
			i, err := strconv.Atoi(value)
			if err != nil {
				return iKw, ParseError{toParse: value, as: "int"}
			}
			r, err := parseKeywordRelation(arg)
			if err != nil {
				return iKw, err
			}
			iKw = IntKeyword{Name: kw, Relation: r, Value: i}
			return iKw, nil
		}
	}
	return iKw, &errorString{s: "no keyword found"}
}

func findSpecialKeywords(args []string) ([]Keyword, []string) {
	keywords := make([]Keyword, 0)
	remaining := make([]string, 0)

	for _, arg := range args {
		kw, err := parseKeyword(arg)
		if err != nil {
			if parseErr, ok := err.(ParseError); ok {
				fmt.Printf("WARNING: %s\n", parseErr)
			}
			remaining = append(remaining, arg)
		} else {
			keywords = append(keywords, kw)
		}
	}

	return keywords, remaining
}

func findInCmdInfo(cmdInfo CommandInfo, keywords []Keyword) bool {
	var comp interface{}
	for _, kw := range keywords {
		switch kw.GetName() {
		case "Walltime":
			comp = cmdInfo.Wall
		case "Usertime":
			comp = cmdInfo.User
		case "Systemtime":
			comp = cmdInfo.System
		case "Exitcode":
			comp = cmdInfo.ExitCode
		case "Signals":
			comp = int(cmdInfo.Resources.Nsignals)
		default:
			continue
		}
		if !kw.Matches(comp) {
			return false
		}
	}
	return true
}

func searchDb(config Config, args []string) (err error) {
	db, err := leveldb.OpenFile(config.Dbfile, nil)
	if err != nil {
		return
	}
	defer db.Close()

	start, end, err := parseStartEnd(args)
	if start.After(end) {
		fmt.Println("WARNING: start date is larger than end date")
	}
	var keywords []string
	if err != nil {
		keywords = args
	} else {
		keywords = args[1:]
	}
	specialKeywords, keywords := findSpecialKeywords(keywords)

	if config.Verbose {
		fmt.Printf("search min: %v\nsearch max: %v\n", start, end)
		fmt.Printf("keywords: %d %v\n", len(keywords), keywords)
		fmt.Printf("special keywords: %d %v\n", len(specialKeywords), specialKeywords)
	}

	lowerBound := []byte(start.UTC().String())
	upperBound := []byte(end.UTC().String())

	iter := db.NewIterator(&util.Range{Start: lowerBound, Limit: upperBound}, nil)
	for iter.Next() {
		start, cmd, err := recoverDbKey(iter.Key())
		if err != nil {
			fmt.Printf("WARNING: %s, skipping entry\n", err)
			continue
		}
		_ = start
		if findInCmdKey(cmd, keywords) {
			cmdInfo, err := recoverDbValue(iter.Value())
			if err != nil {
				fmt.Printf("WARNING: %s, skipping entry\n", err)
				continue
			}
			if findInCmdInfo(cmdInfo, specialKeywords) {
				fmt.Printf("%s\t%s\t= %s\n", start.In(time.Local).Format("2006-01-02 15:04:05"),
					cmd, parseDuration(cmdInfo))
			}
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
	if err != nil {
		return CommandInfo{}, err
	}
	signal.Ignore(syscall.SIGHUP, syscall.SIGINT)
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

func init() {
	var err error
	currentUser, err = user.Current()
	if err != nil {
		fmt.Printf("ERROR: %s\n", err)
		os.Exit(1)
	}
}

func main() {
	defaultDb := path.Join(currentUser.HomeDir, ".timedatabase")

	printVersion := flag.Bool("version", false, "print version and exit")
	verbose := flag.Bool("verbose", false, "print additional info during run")
	dbFile := flag.String("dbfile", defaultDb, "specify which time database to use")
	dump := flag.Bool("dump", false, "print the whole database")
	search := flag.Bool("search", false, "search in the database. use -search <timerange> <keywords>*")
	kwHelp := flag.Bool("keywordhelp", false, "print help about search keywords and exit")
	license := flag.Bool("license", false, "print license details and exit")
	flag.Parse()

	config := Config{
		Verbose: *verbose,
		Dbfile:  *dbFile,
	}
	cmdArgs := flag.Args()

	if *printVersion {
		fmt.Printf("%s\n", version)
		return
	}

	if *kwHelp {
		fmt.Println(kwHelpText)
		return
	}

	if *license {
		fmt.Println(licenseHelp)
		return
	}

	if config.Verbose {
		fmt.Printf("version = %s\n", version)
		fmt.Printf("dbfile = %s\n", config.Dbfile)
	}

	if *dump {
		err := printDb(config)
		if err != nil {
			fmt.Printf("ERROR: %s", err)
			os.Exit(1)
		}
		return
	}
	if *search {
		err := searchDb(config, cmdArgs)
		if err != nil {
			fmt.Printf("ERROR: %s", err)
			os.Exit(1)
		}
		return
	}
	if len(cmdArgs) == 0 {
		fmt.Println("No command to measure given. Exiting ...")
		return
	}
	cmdInfo, err := run(config, cmdArgs)
	if err != nil {
		fmt.Printf("ERROR: %s", err)
		os.Exit(1)
	}
	printDuration(cmdInfo)

	err = storeCmd(config, cmdInfo)
	if err != nil {
		fmt.Printf("ERROR: %s", err)
		os.Exit(1)
	}
}
