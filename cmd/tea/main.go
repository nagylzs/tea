package main

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"github.com/fatih/color"
	"github.com/nagylzs/tea/internal/opts"
	"golang.org/x/sys/unix"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
)

//go:embed USAGE.txt
var Usage string

type Main struct {
	Opts          opts.Type
	Cmd           *exec.Cmd
	StdIn         io.WriteCloser
	StdOut        io.ReadCloser
	StdErr        io.ReadCloser
	FixedExitCode *atomic.Int32
}

type Line struct {
	Value      string
	InStdErr   bool // the line came from stderr instead of stdout
	OutStdErr  bool // the line should be written to stderr
	MarkStdOut *string
	MarkStdErr *string
	Prefix     *string
	Suffix     *string
}

type LineChannel = chan Line

var NewLine = "\n"

func ListSignals() {
	// https://stackoverflow.com/questions/42598522/how-can-i-list-available-operating-system-signals-by-name-in-a-cross-platform-wa
	for i := syscall.Signal(0); i < syscall.Signal(255); i++ {
		name := unix.SignalName(i)
		if name != "" {
			fmt.Printf("%s = %d\n", name, i)
		}
	}
}

var m Main

func main() {
	o, err := opts.ParseArgs()
	if err != nil {
		log.Fatal(err)
	}
	if o.Help {
		fmt.Println(Usage)
		os.Exit(0)
	}
	if o.ListSignals {
		ListSignals()
		os.Exit(0)
	}

	cmd := exec.Command(o.Program, o.ProgramArgs...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}
	m = Main{
		Opts:          o,
		Cmd:           cmd,
		StdIn:         stdin,
		StdOut:        stdout,
		StdErr:        stderr,
		FixedExitCode: &atomic.Int32{},
	}
	m.FixedExitCode.Store(-1)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	if o.PidFile != "" {
		err = os.WriteFile(o.PidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			err := os.Remove(o.PidFile)
			if err != nil {
				log.Fatal(err)
			}
		}()
	}

	chStdInIn := make(chan string, 1)
	//go ReadStdIn(os.Stdin, m.Opts.LineBufferSize, chStdInIn)
	//go WriteData(m.StdIn, chStdInIn, nil)

	chStdOutOut := make(chan string, 1)
	chStdErrOut := make(chan string, 1)
	chStdOutIn := make(LineChannel, 1)
	chStdErrIn := make(LineChannel, 1)

	wgProc := sync.WaitGroup{}

	if o.ShareStreams {
		// share streams: read from stdout and stderr, and put both of them into chStdOutIn
		wgRead := sync.WaitGroup{}
		wgRead.Add(2)
		go ReadLines(m.StdOut, m.Opts.LineBufferSize, false, chStdOutIn, &wgRead)
		go ReadLines(m.StdErr, m.Opts.LineBufferSize, false, chStdOutIn, &wgRead)
		go func() {
			wgRead.Wait()
			close(chStdOutIn)
		}()
		// Only chStdOutIn is used
		wgProc.Add(1)
		go ProcessLines(&o.Commands, o.CmdIdx, chStdInIn, chStdOutIn, chStdOutOut, chStdErrOut, &wgProc)
	} else {
		// normal: read from stdout and stderr, and put them into chStdOutIn and chStdErrIn
		go ReadLines(m.StdOut, m.Opts.LineBufferSize, false, chStdOutIn, nil)
		go ReadLines(m.StdErr, m.Opts.LineBufferSize, true, chStdErrIn, nil)

		if o.ShareCommands {
			// Merge chStdOutIn and chStdErrIn into chIn
			chIn := make(chan Line)
			wg := sync.WaitGroup{}
			wg.Add(2)
			go func() {
				for line := range chStdOutIn {
					chIn <- line
				}
				wg.Done()
			}()
			go func() {
				for line := range chStdErrIn {
					chIn <- line
				}
				wg.Done()
			}()
			// Wait until both closed, then close chIn
			go func() {
				wg.Wait()
				close(chIn)
			}()
			// Process serialized lines with the same command chain
			wgProc.Add(1)
			go ProcessLines(&o.Commands, o.CmdIdx, chStdInIn, chIn, chStdOutOut, chStdErrOut, &wgProc)
		} else {
			// Process stdin and stdout with different command chain instances
			cmdStdOut := o.Commands
			cmdStdErr := o.Commands
			wgProc.Add(2)
			go ProcessLines(&cmdStdOut, o.CmdIdx, chStdInIn, chStdOutIn, chStdOutOut, chStdErrOut, &wgProc)
			go ProcessLines(&cmdStdErr, o.CmdIdx, chStdInIn, chStdErrIn, chStdOutOut, chStdErrOut, &wgProc)
		}

	}

	go func() {
		wgProc.Wait()
		close(chStdOutOut)
		close(chStdErrOut)
	}()

	wgWrite := sync.WaitGroup{}
	wgWrite.Add(2)
	go WriteData(os.Stdout, chStdOutOut, &wgWrite)
	go WriteData(os.Stderr, chStdErrOut, &wgWrite)

	wgWrite.Wait()
	err = cmd.Wait()

	ec := m.FixedExitCode.Load()
	if ec >= 0 {
		os.Exit(int(ec))
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			fmt.Println(exitErr.Error())
			os.Exit(exitErr.ExitCode())
		}
	}
}

func ReadLines(reader io.ReadCloser, bufSize int, inStdErr bool, ch LineChannel, wgRead *sync.WaitGroup) {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, bufSize)
	scanner.Buffer(buf, bufSize)
	for scanner.Scan() {
		line := scanner.Text()
		ch <- Line{line, inStdErr, inStdErr, nil, nil, nil, &NewLine}
	}
	if wgRead == nil {
		close(ch)
	} else {
		wgRead.Done()
	}
}

func WriteData(writer io.WriteCloser, ch chan string, wg *sync.WaitGroup) {
	for line := range ch {
		_, err := writer.Write([]byte(line))
		if err != nil {
			log.Fatal(err)
		}
	}
	if wg != nil {
		wg.Done()
	}
}

func ProcessLines(commands *[]opts.Command, CmdIdx map[string]int, chStdInIn chan string, chIn LineChannel, chStdOutOut chan string, chStdErrOut chan string, wgProc *sync.WaitGroup) {
	for line := range chIn {
		// Perform LineEnabled / LineDisabled at the beginning of the line
		for _, cmd := range *commands {
			if cmd.LineEnabled {
				cmd.Disabled = false
			} else if cmd.LineDisabled {
				cmd.Disabled = true
			}
		}
		// go over all commands
		cmdIdx := 0
		closeStdIn := false
		var clr *color.Color = nil
		for cmdIdx < len(*commands) {

			// eval conditions

			cmd := (*commands)[cmdIdx]
			cmdIdx++
			if cmd.Disabled { // skip disabled commands
				continue
			}
			if line.InStdErr && !cmd.Conditions.StdErr { // skip by input source filter
				continue
			}
			if !line.InStdErr && !cmd.Conditions.StdOut { // skip by input source filter
				continue
			}
			// pattern matching
			if !commandLineMatch(&line, &cmd) {
				continue
			}
			/* TODO: process time based conditions

			AndTimeout         *time.Duration
			OrTimeout          *time.Duration
			MinMatchTime       *time.Duration
			NoInputForDuration *time.Duration

			The AndTimeout and OrTimeout could be implemented using a special Line object, that has nil line Value.
			and emitted when the timeout is reached. The ProcessLines() method should save the "last match state"
			of each command, and process these special lines using the "last match state" as the condition.

			Steps to implement:

			1. Make Line.value *string instead of string
			2. Add command index reference Line.cmdIdx
			3. When processing starts, create a new go routine for each command with a timeout, and emit a special
			   line(s) when the timeout is reached. The emission target can be std-out-in or std-err-in, depending
			   on the command's settings.
			4. Rewrite ProcessLines, detect these special lines and treat them as timeout events. Process the action
			   if the timeout has come, and the last state is "matched".
			5. Think over what should happen when a timeout based command is enabled AFTER its timeout has come.
			   Should the "enable" operation trigger its actions immediately or not?

			The above should work for AndTimeout and OrTimeout. Test this, and only after that should we implement
			MinMatchTime, MaxMatchTime, InputForDuration and NoInputForDuration.

			*/

			// process actions
			a := cmd.Actions
			if a.MarkStdOut != nil {
				line.MarkStdOut = a.MarkStdOut
			}
			if a.MarkStdErr != nil {
				line.MarkStdErr = a.MarkStdErr
			}
			if a.SendToStdOut {
				line.OutStdErr = false
			}
			if a.SendToStdErr {
				line.OutStdErr = true
			}
			if a.SetPrefix != nil {
				line.Prefix = a.SetPrefix
			}
			if a.SetSuffix != nil {
				line.Suffix = a.SetSuffix
			}
			if a.Color != nil {
				clr = a.Color
			}

			if a.Signal != nil {
				if err := syscall.Kill(m.Cmd.Process.Pid, *a.Signal); err != nil {
					log.Fatal(err)
				}
			}

			if a.Input != nil {
				chStdInIn <- *a.Input
			}

			if a.InputFile != nil {
				log.Fatal("--send-input-file not yet implemented, need to refactor ForwardStdIn")
			}

			if a.CloseStdIn {
				closeStdIn = true
			}

			if a.SetExitCode != nil {
				m.FixedExitCode.Store(*a.SetExitCode)
			}

			if a.ClearExitCode {
				m.FixedExitCode.Store(-1)
			}

			for _, n := range a.Disable {
				i, ok := CmdIdx[n]
				if !ok {
					log.Fatal(fmt.Errorf("inernal error: --disable references to non-existent command %v", n))
				}
				(*commands)[i].Disabled = true
			}

			for _, n := range a.Enable {
				i, ok := CmdIdx[n]
				if !ok {
					log.Fatal(fmt.Errorf("inernal error: --enable references to non-existent command %v", n))
				}
				(*commands)[i].Disabled = true
			}

			for _, n := range a.Toggle {
				i, ok := CmdIdx[n]
				if !ok {
					log.Fatal(fmt.Errorf("inernal error: --toggle references to non-existent command %v", n))
				}
				(*commands)[i].Disabled = !(*commands)[i].Disabled
			}

			if a.NextLine {
				break
			}

			if a.SkipTo != nil {
				i, ok := CmdIdx[*a.SkipTo]
				if !ok {
					log.Fatal(fmt.Errorf("inernal error: --skip-to references to non-existent command %v", a.SkipTo))
				}
				cmdIdx = i
				continue
			}

		}

		if closeStdIn {
			if err := m.StdIn.Close(); err != nil {
				log.Fatal(err)
			}
		}

		var format = func(fmt string, a ...interface{}) string {
			return fmt
		}
		if clr != nil {
			format = clr.SprintfFunc()
		}

		if line.OutStdErr {
			if line.MarkStdErr != nil {
				chStdErrOut <- format(*line.MarkStdErr)
			} else {
				if line.Prefix != nil {
					chStdErrOut <- format(*line.Prefix)
				}
				chStdErrOut <- line.Value
				if line.Suffix != nil {
					chStdErrOut <- format(*line.Suffix)
				}
			}
		} else {
			if line.MarkStdOut != nil {
				chStdOutOut <- format(*line.MarkStdOut)
			} else {
				if line.Prefix != nil {
					chStdOutOut <- format(*line.Prefix)
				}
				chStdOutOut <- format(line.Value)
				if line.Suffix != nil {
					chStdOutOut <- format(*line.Suffix)
				}
			}

		}
	}
	wgProc.Done()
}

func commandLineMatch(l *Line, o *opts.Command) bool {
	if o.Conditions.No {
		if o.Conditions.Or {
			// --or --no will match if at least pattern does not match
			for _, pat := range o.Conditions.CompiledPatterns {
				if !pat.MatchString(l.Value) {
					return true
				}
			}
			return false
		} else {
			// --no will match if none of the patterns match
			for _, pat := range o.Conditions.CompiledPatterns {
				if pat.MatchString(l.Value) {
					return false
				}
			}
			return true
		}
	} else {
		if o.Conditions.Or {
			// --or will match if at least one pattern matches
			for _, pat := range o.Conditions.CompiledPatterns {
				if pat.MatchString(l.Value) {
					return true
				}
			}
			return false
		} else {
			// default: all patterns must match
			for _, pat := range o.Conditions.CompiledPatterns {
				if !pat.MatchString(l.Value) {
					return false
				}
			}
			return true
		}
	}
}
