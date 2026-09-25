package main

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/nagylzs/tea/internal/opts"
	"github.com/nagylzs/tea/internal/version"
	"golang.org/x/sys/unix"
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
	Value     string
	InStdErr  bool    // the line came from stderr instead of stdout
	OutStdErr bool    // the line should be written to stderr
	Mark      *string // replaces the line (and prefix/suffix) on output; chosen by the input stream
	Prefix    *string
	Suffix    *string
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
	if o.ShowVersion {
		version.PrintVersion()
		os.Exit(0)
	}
	if o.ListSignals {
		ListSignals()
		os.Exit(0)
	}

	if o.StdBufMissing {
		log.Printf("warning: stdbuf not found, starting PROGRAM directly; its output may be block-buffered (see --no-stdbuf in --help)")
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

	// --send-input and --close requests from all processors go through one
	// writer goroutine so that they reach the child's stdin in order.
	chStdInIn := make(chan stdInRequest)
	go WriteStdIn(m.StdIn, chStdInIn)
	if !o.NoStdIn {
		go ForwardStdIn(os.Stdin, o.LineBufferSize, chStdInIn)
	}

	chStdOutOut := make(chan string, 1)
	chStdErrOut := make(chan string, 1)
	chStdOutIn := make(LineChannel, 1)
	chStdErrIn := make(LineChannel, 1)

	// One instance of every command. Chains share the timed commands and get
	// their own copies of the line commands (see newChain).
	shared := make(Chain, len(o.Commands))
	for i := range o.Commands {
		shared[i] = &o.Commands[i]
		shared[i].ResetStarted()
	}
	lastLine[0] = time.Now()
	lastLine[1] = lastLine[0]
	var chains []Chain

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
		chains = []Chain{shared}
		wgProc.Add(1)
		go ProcessLines(shared, o.CmdIdx, chStdInIn, chStdOutIn, chStdOutOut, chStdErrOut, &wgProc)
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
			chains = []Chain{shared}
			wgProc.Add(1)
			go ProcessLines(shared, o.CmdIdx, chStdInIn, chIn, chStdOutOut, chStdErrOut, &wgProc)
		} else {
			// Process stdout and stderr with different command chain instances
			cmdStdOut := newChain(shared)
			cmdStdErr := newChain(shared)
			chains = []Chain{cmdStdOut, cmdStdErr}
			wgProc.Add(2)
			go ProcessLines(cmdStdOut, o.CmdIdx, chStdInIn, chStdOutIn, chStdOutOut, chStdErrOut, &wgProc)
			go ProcessLines(cmdStdErr, o.CmdIdx, chStdInIn, chStdErrIn, chStdOutOut, chStdErrOut, &wgProc)
		}

	}

	// Timed commands are evaluated once per second, independently of the chains.
	timerDone := make(chan struct{})
	wgTimer := sync.WaitGroup{}
	wgTimer.Add(1)
	go RunTimers(chains, o.CmdIdx, chStdInIn, timerDone, &wgTimer)

	go func() {
		wgProc.Wait()
		// stop the timers before the child is reaped, so a late --signal cannot fail
		close(timerDone)
		wgTimer.Wait()
		close(chStdOutOut)
		close(chStdErrOut)
	}()

	wgWrite := sync.WaitGroup{}
	wgWrite.Add(2)
	go WriteData(os.Stdout, chStdOutOut, &wgWrite)
	go WriteData(os.Stderr, chStdErrOut, &wgWrite)

	wgWrite.Wait()
	// Let queued --send-input data reach the child before Wait closes its stdin.
	flushed := make(chan struct{})
	chStdInIn <- stdInRequest{done: flushed}
	<-flushed
	err = cmd.Wait()

	ec := m.FixedExitCode.Load()
	if ec >= 0 {
		os.Exit(int(ec))
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, exitErr.Error())
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
		ch <- Line{line, inStdErr, inStdErr, nil, nil, &NewLine}
	}
	if err := scanner.Err(); err != nil {
		// e.g. bufio.ErrTooLong when a line exceeds --line-buffer-size
		log.Fatal(err)
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

// stdInRequest is one item for the child's stdin: data to write, a request to
// close the pipe, or a flush marker.
type stdInRequest struct {
	data      string
	close     bool
	forwarded bool          // data copied from tea's own stdin: dropped silently once stdin is gone
	done      chan struct{} // flush marker: closed once everything queued before it was handled
}

// unboundedQueue returns a channel that yields everything sent to in, in order,
// while never blocking the sender. Line processing must not block on the child
// reading its stdin: if it did, tea would stop reading the child's stdout, and
// a child that writes a lot before it reads stdin would deadlock with tea.
func unboundedQueue(in <-chan stdInRequest) <-chan stdInRequest {
	out := make(chan stdInRequest)
	go func() {
		defer close(out)
		var queue []stdInRequest
		for in != nil || len(queue) > 0 {
			var send chan<- stdInRequest
			var head stdInRequest
			if len(queue) > 0 {
				send = out
				head = queue[0]
			}
			select {
			case req, ok := <-in:
				if !ok {
					in = nil
					continue
				}
				queue = append(queue, req)
			case send <- head:
				queue = queue[1:]
			}
		}
	}()
	return out
}

// WriteStdIn serves the child's stdin: forwarded input, --send-input and
// --close requests, in the order they were issued. It runs until tea exits.
// Once stdin is closed (by --close, by end of file on tea's own stdin, or
// because the child exited) further --send-input data is dropped with a
// warning; forwarded data is dropped silently, as in an ordinary pipeline.
func WriteStdIn(w io.WriteCloser, ch chan stdInRequest) {
	closed := false
	for req := range unboundedQueue(ch) {
		switch {
		case req.done != nil:
			close(req.done)
		case req.close && closed:
			// closing twice is harmless
		case req.close:
			closed = true
			if err := w.Close(); err != nil {
				log.Printf("warning: cannot close stdin of PROGRAM: %v", err)
			}
		case closed:
			if !req.forwarded {
				log.Printf("warning: --send-input dropped, stdin of PROGRAM is already closed")
			}
		default:
			if _, err := io.WriteString(w, req.data); err != nil {
				// the child went away (or closed its stdin): treat stdin as closed from now on
				closed = true
				if !req.forwarded {
					log.Printf("warning: cannot write to stdin of PROGRAM: %v", err)
				}
			}
		}
	}
}

// ForwardStdIn copies tea's own stdin to the child through the stdin writer.
// It copies chunks as they arrive, not lines, so partial lines and binary data
// pass through unchanged. End of file (or a read error) closes the child's
// stdin, exactly as if PROGRAM had been started without tea.
func ForwardStdIn(r io.Reader, bufSize int, ch chan<- stdInRequest) {
	buf := make([]byte, bufSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			ch <- stdInRequest{data: string(buf[:n]), forwarded: true}
		}
		if err != nil {
			ch <- stdInRequest{close: true}
			return
		}
	}
}

// Chain is one command chain: the commands in order, as pointers so that a
// timed command can be one shared instance across chains (see newChain).
type Chain []*opts.Command

// stateMu guards all mutable command state (Disabled, Started, Fired) and
// lastLine. A chain processor holds it while it evaluates a line, the timer
// goroutine while it evaluates the timed commands. Output is written outside
// the lock.
var stateMu sync.Mutex

// lastLine is when the last line arrived on stdout (0) and stderr (1).
var lastLine [2]time.Time

// newChain returns the chain for one stream. Line commands are deep copies so
// that --disable/--enable/--toggle state is independent per stream (as
// USAGE.txt promises); timed commands are shared, because there is exactly one
// instance of each timed command no matter how many chains there are.
func newChain(shared Chain) Chain {
	chain := make(Chain, len(shared))
	for i, c := range shared {
		if c.IsTimed() {
			chain[i] = c
		} else {
			cp := c.Clone()
			chain[i] = &cp
		}
	}
	return chain
}

// setDisabled changes a command's state. Enabling a command that was disabled
// (re)arms its --timeout: the deadline counts from when it was last enabled.
func setDisabled(c *opts.Command, disabled bool) {
	if c.Disabled && !disabled {
		c.ResetStarted()
		c.Fired = false
	}
	c.Disabled = disabled
}

// ProcessLines runs the chain over every line of chIn.
func ProcessLines(chain Chain, cmdIndices map[string]int, chStdInIn chan stdInRequest, chIn LineChannel, chStdOutOut chan string, chStdErrOut chan string, wgProc *sync.WaitGroup) {
	defer wgProc.Done()
	for line := range chIn {
		processLine(chain, cmdIndices, chStdInIn, line, chStdErrOut, chStdOutOut)
	}
}

// RunTimers evaluates the timed commands (--no-input-for, --timeout) once per
// second until done is closed. Timed commands are evaluated here and only
// here, so each fires once per tick at most, and their --disable/--enable/
// --toggle actions apply to every chain.
func RunTimers(chains []Chain, cmdIndices map[string]int, chStdInIn chan stdInRequest, done <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			evaluateTimers(chains, cmdIndices, chStdInIn, now)
		}
	}
}

func evaluateTimers(chains []Chain, cmdIndices map[string]int, chStdInIn chan stdInRequest, now time.Time) {
	stateMu.Lock()
	defer stateMu.Unlock()

	last := lastLine[0]
	if lastLine[1].After(last) {
		last = lastLine[1]
	}
	idle := now.Sub(last)

	chain := chains[0] // timed commands are shared, so any chain will do
	closeStdIn := false
	cmdIdx := 0
	for cmdIdx < len(chain) {
		cmd := chain[cmdIdx]
		cmdIdx++
		if !cmd.IsTimed() || cmd.Disabled {
			continue
		}
		c := cmd.Conditions
		fire := false
		if c.NoInputFor != nil && !cmd.Fired && idle >= *c.NoInputFor {
			// fires once per quiet period: Fired is cleared when a line arrives
			cmd.Fired = true
			fire = true
		}
		if c.Timeout != nil && !cmd.Fired && now.Sub(cmd.Started) >= *c.Timeout {
			// fires once per arming, see setDisabled
			cmd.Fired = true
			fire = true
		}
		if !fire {
			continue
		}
		if applyActions(chains, cmdIndices, chStdInIn, cmd.Actions, &cmdIdx, &closeStdIn) {
			break
		}
	}
	if closeStdIn {
		chStdInIn <- stdInRequest{close: true}
	}
}

// targets returns the distinct instances of the command named n across the
// chains: one per chain for a line command, a single one for a timed command.
func targets(chains []Chain, cmdIndices map[string]int, n string, opt string) []*opts.Command {
	i, ok := cmdIndices[n]
	if !ok {
		log.Fatal(fmt.Errorf("internal error: %s references to non-existent command %v", opt, n))
	}
	var out []*opts.Command
	for _, chain := range chains {
		c := chain[i]
		seen := false
		for _, o := range out {
			if o == c {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, c)
		}
	}
	return out
}

// applyActions performs the non-line-specific actions of a command: signals, stdin
// input, exit code overrides, --disable/--enable/--toggle, --next-line and --skip-to.
// It is shared by processLine and evaluateTimers. chains are the chains the
// state-changing actions apply to: the processor's own chain for a line
// command, every chain for a timed command. cmdIdx is the index of the *next*
// command to evaluate and is rewritten by --skip-to. closeStdIn is set when
// --close is requested; the caller queues the close after the loop. It returns
// true when the caller must stop evaluating further commands (--next-line).
// The caller holds stateMu.
func applyActions(chains []Chain, cmdIndices map[string]int, chStdInIn chan stdInRequest, a *opts.CommandActions, cmdIdx *int, closeStdIn *bool) bool {
	if a.Signal != nil {
		if err := syscall.Kill(m.Cmd.Process.Pid, *a.Signal); err != nil {
			log.Fatal(err)
		}
	}

	if a.Input != nil {
		chStdInIn <- stdInRequest{data: *a.Input}
	}

	if a.InputFile != nil {
		// read at execution time: the file need not exist when tea starts
		content, err := os.ReadFile(*a.InputFile)
		if err != nil {
			log.Fatalf("--send-input-file: %v", err)
		}
		chStdInIn <- stdInRequest{data: string(content)}
	}

	if a.CloseStdIn {
		*closeStdIn = true
	}

	if a.SetExitCode != nil {
		m.FixedExitCode.Store(*a.SetExitCode)
	}

	if a.ClearExitCode {
		m.FixedExitCode.Store(-1)
	}

	if a.Exit != nil {
		// --exit CODE: stop PROGRAM with the stop signal and use CODE as tea's exit code
		if err := syscall.Kill(m.Cmd.Process.Pid, m.Opts.StopSignal); err != nil {
			log.Fatal(err)
		}
		m.FixedExitCode.Store(*a.Exit)
	}

	for _, n := range a.Disable {
		for _, c := range targets(chains, cmdIndices, n, "--disable") {
			setDisabled(c, true)
		}
	}

	for _, n := range a.Enable {
		for _, c := range targets(chains, cmdIndices, n, "--enable") {
			setDisabled(c, false)
		}
	}

	for _, n := range a.Toggle {
		for _, c := range targets(chains, cmdIndices, n, "--toggle") {
			setDisabled(c, !c.Disabled)
		}
	}

	if a.NextLine {
		return true
	}

	if a.SkipTo != nil {
		i, ok := cmdIndices[*a.SkipTo]
		if !ok {
			log.Fatal(fmt.Errorf("internal error: --skip-to references to non-existent command %v", *a.SkipTo))
		}
		*cmdIdx = i
	}
	return false
}

func processLine(chain Chain, cmdIndices map[string]int, chStdInIn chan stdInRequest, line Line, chStdErrOut chan string, chStdOutOut chan string) {
	stateMu.Lock()

	stream := 0
	if line.InStdErr {
		stream = 1
	}
	lastLine[stream] = time.Now()
	// a new line ends the quiet period: re-arm the idle commands
	for _, cmd := range chain {
		if cmd.Conditions.NoInputFor != nil {
			cmd.Fired = false
		}
	}

	// Perform LineEnabled / LineDisabled at the beginning of the line
	for _, cmd := range chain {
		if cmd.LineEnabled {
			setDisabled(cmd, false)
		} else if cmd.LineDisabled {
			setDisabled(cmd, true)
		}
	}
	// go over all commands
	chains := []Chain{chain}
	cmdIdx := 0
	closeStdIn := false
	var clr *color.Color = nil
	for cmdIdx < len(chain) {

		// eval conditions

		cmd := chain[cmdIdx]
		cmdIdx++

		// timed commands have no current line; RunTimers evaluates them
		if cmd.IsTimed() {
			continue
		}

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
		if !commandLineMatch(&line, cmd) {
			continue
		}

		// process line-specific actions ("last one wins")
		a := cmd.Actions
		// --mark applies to lines read from stdout, --mark-stderr to lines read from
		// stderr, regardless of where --send-to-stdout/--send-to-stderr routes them.
		if a.MarkStdOut != nil && !line.InStdErr {
			line.Mark = a.MarkStdOut
		}
		if a.MarkStdErr != nil && line.InStdErr {
			line.Mark = a.MarkStdErr
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

		// process shared actions
		if applyActions(chains, cmdIndices, chStdInIn, a, &cmdIdx, &closeStdIn) {
			break
		}
	}

	if closeStdIn {
		// queued after this line's --send-input requests, so they are written first
		chStdInIn <- stdInRequest{close: true}
	}

	stateMu.Unlock()

	var format = func(fmt string, a ...interface{}) string {
		return fmt
	}
	if clr != nil {
		format = clr.SprintfFunc()
	}

	// Build the whole output line first and hand it over in one piece, so that
	// one write(2) carries it: with stdout and stderr on the same terminal, the
	// two writer goroutines can then interleave lines but never parts of lines.
	var text string
	if line.Mark != nil {
		text = format(*line.Mark)
	} else {
		if line.Prefix != nil {
			text += format(*line.Prefix)
		}
		text += format(line.Value)
		if line.Suffix != nil {
			text += format(*line.Suffix)
		}
	}
	if line.OutStdErr {
		chStdErrOut <- text
	} else {
		chStdOutOut <- text
	}
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
