package opts

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/fatih/color"
)

type Type struct {
	Help           bool
	ShowVersion    bool
	ListSignals    bool
	PidFile        string
	LineBufferSize int
	NoStdBuf       bool
	StdBufMissing  bool // stdbuf was wanted but is not on PATH; PROGRAM is started directly
	NoStdIn        bool
	StopSignal     syscall.Signal
	Deadline       *time.Duration
	ShareCommands  bool
	ShareStreams   bool
	Commands       []Command
	CmdIdx         map[string]int
	Program        string
	ProgramArgs    []string
}

var Opts = defaultOpts()

func defaultOpts() Type {
	return Type{LineBufferSize: 65535, StopSignal: syscall.SIGTERM, Commands: make([]Command, 0)}
}

var argIdx = 0  // arg index
var cmdIdx = -1 // block index

type Option int

const (
	Help Option = iota
	ShowVersion
	ListSignals
	PID
	LineBufferSize
	NoStdBuf
	NoStdIn
	StopSignal
	Deadline
	ShareCommands
	ShareStreams
	NewCommand
	Disabled
	LineDisabled
	LineEnabled
	Pattern
	Or
	No
	StdErr
	StdAll
	Timeout
	OrTimeout
	MinMatchTime
	NoInputFor
	MarkStdout
	MarkStdErr
	SetPrefix
	SetSuffix
	FgColor
	BgColor
	Underline
	Bold
	Italic
	Faint
	BlinkSlow
	BlinkRapid
	SendToStdOut
	SendToStdErr
	Next
	SkipTo
	Disable
	Enable
	Toggle
	Signal
	SendInput
	SendInputFile
	CloseStdin
	SetExitCode
	ClearExitCode
	Exit
)

var shortOptions = map[string]Option{
	"-h": Help,
	"-l": ListSignals,
	"-c": NewCommand,
	"-p": Pattern,
	"-a": StdAll,
	"-t": Timeout,
	"-m": MarkStdout,
	"-n": Next,
	"-s": Signal,
	"-i": SendInput,
	"-f": SendInputFile,
	"-e": SetExitCode,
	"-x": Exit,
}

var longOptions = map[string]Option{
	"--help":             Help,
	"--version":          ShowVersion,
	"--list-signals":     ListSignals,
	"--pid":              PID,
	"--line-buffer-size": LineBufferSize,
	"--no-stdbuf":        NoStdBuf,
	"--no-stdin":         NoStdIn,
	"--stop-signal":      StopSignal,
	"--deadline":         Deadline,
	"--share-commands":   ShareCommands,
	"--share-streams":    ShareStreams,
	"--command":          NewCommand,
	"--disabled":         Disabled,
	"--line-disabled":    LineDisabled,
	"--line-enabled":     LineEnabled,
	"--pattern":          Pattern,
	"--or":               Or,
	"--no":               No,
	"--std-err":          StdErr,
	"--std-all":          StdAll,
	"--timeout":          Timeout,
	"--or-timeout":       OrTimeout,
	"--min-match-time":   MinMatchTime,
	"--no-input-for":     NoInputFor,
	"--mark":             MarkStdout,
	"--mark-stderr":      MarkStdErr,
	"--set-prefix":       SetPrefix,
	"--set-suffix":       SetSuffix,
	"--fg-color":         FgColor,
	"--bg-color":         BgColor,
	"--underline":        Underline,
	"--bold":             Bold,
	"--faint":            Faint,
	"--italic":           Italic,
	"--blink":            BlinkSlow,
	"--blink-rapid":      BlinkRapid,
	"--send-to-stdout":   SendToStdOut,
	"--send-to-stderr":   SendToStdErr,
	"--next-line":        Next,
	"--skip-to":          SkipTo,
	"--disable":          Disable,
	"--enable":           Enable,
	"--toggle":           Toggle,
	"--signal":           Signal,
	"--send-input":       SendInput,
	"--send-input-file":  SendInputFile,
	"--close":            CloseStdin,
	"--set-exit-code":    SetExitCode,
	"--clear-exit-code":  ClearExitCode,
	"--exit":             Exit,
}

func internalParseArgs() error {
	if len(os.Args) == 1 {
		Opts.Help = true
		return nil
	}

	argIdx = 0
	dDash := false
	for argIdx+1 < len(os.Args) {
		argIdx++
		arg := os.Args[argIdx]
		if arg == "--" {
			dDash = true
			break
		}
		opt, ok := longOptions[arg]
		if !ok {
			opt, ok = shortOptions[arg]
		}
		if !ok {
			return fmt.Errorf("invalid command: %v", arg)
		}
		err2 := error(nil)

		if !isGlobalOption(opt) && opt != NewCommand && cmdIdx < 0 {
			// the first command is implicit: a command level option starts it
			addEmptyCommand()
		}
		switch opt {
		case Help:
			Opts.Help = true
			return nil
		case ShowVersion:
			Opts.ShowVersion = true
			return nil
		case ListSignals:
			Opts.ListSignals = true
			return nil
		case PID:
			Opts.PidFile, err2 = popStringArg("--pid")
		case LineBufferSize:
			Opts.LineBufferSize, err2 = popIntArg("--line-buffer-size")
		case NoStdBuf:
			Opts.NoStdBuf = true
		case NoStdIn:
			Opts.NoStdIn = true
		case Deadline:
			Opts.Deadline, err2 = popDurationArg(arg)
		case StopSignal:
			var sig *syscall.Signal
			sig, err2 = popSignalPArg(arg)
			if err2 == nil {
				Opts.StopSignal = *sig
			}
		case ShareCommands:
			Opts.ShareCommands = true
		case ShareStreams:
			Opts.ShareStreams = true
		case NewCommand:
			addEmptyCommand()
			currentCommand().Name, err2 = popOptName("command")
		case Disabled:
			currentCommand().Disabled = true
		case LineDisabled:
			currentCommand().LineDisabled = true
		case LineEnabled:
			currentCommand().LineEnabled = true
		case Pattern:
			err2 = addPattern(arg)
		case Or:
			currentConditions().Or = true
		case No:
			currentConditions().No = true
		case StdErr:
			currentConditions().StdOut = false
			currentConditions().StdErr = true
		case StdAll:
			currentConditions().StdOut = true
			currentConditions().StdErr = true
		case Timeout:
			currentConditions().Timeout, err2 = popDurationArg(arg)
		case OrTimeout:
			currentConditions().OrTimeout, err2 = popDurationArg(arg)
		case MinMatchTime:
			currentConditions().MinMatchTime, err2 = popDurationArg(arg)
		case NoInputFor:
			currentConditions().NoInputFor, err2 = popDurationArg(arg)
		case MarkStdout:
			currentActions().MarkStdOut, err2 = popStringPArg(arg)
		case MarkStdErr:
			currentActions().MarkStdErr, err2 = popStringPArg(arg)
		case SetPrefix:
			currentActions().SetPrefix, err2 = popStringPArg(arg)
		case SetSuffix:
			currentActions().SetSuffix, err2 = popStringPArg(arg)
		case FgColor:
			var attr color.Attribute
			attr, err2 = popColorFgAttrArg(arg)
			if err2 == nil {
				changeColorAttribute(attr)
			}
		case BgColor:
			var attr color.Attribute
			attr, err2 = popColorBgAttrArg(arg)
			if err2 == nil {
				changeColorAttribute(attr)
			}
		case Bold:
			changeColorAttribute(color.Bold)
		case Italic:
			changeColorAttribute(color.Italic)
		case Faint:
			changeColorAttribute(color.Faint)
		case Underline:
			changeColorAttribute(color.Underline)
		case BlinkSlow:
			changeColorAttribute(color.BlinkSlow)
		case BlinkRapid:
			changeColorAttribute(color.BlinkRapid)
		case SendToStdOut:
			currentActions().SendToStdOut = true
		case SendToStdErr:
			currentActions().SendToStdErr = true
		case Next:
			currentActions().NextLine = true
		case SkipTo:
			currentActions().SkipTo, err2 = popNamePArg(arg)
		case Disable:
			err2 = appendNameArg(arg, &currentActions().Disable)
		case Enable:
			err2 = appendNameArg(arg, &currentActions().Enable)
		case Toggle:
			err2 = appendNameArg(arg, &currentActions().Toggle)
		case Signal:
			currentActions().Signal, err2 = popSignalPArg(arg)
		case SendInput:
			currentActions().Input, err2 = popStringPArg(arg)
		case SendInputFile:
			currentActions().InputFile, err2 = popStringPArg(arg)
		case CloseStdin:
			currentActions().CloseStdIn = true
		case SetExitCode:
			currentActions().SetExitCode, err2 = popExitCodePArg(arg)
		case ClearExitCode:
			currentActions().ClearExitCode = true
		case Exit:
			currentActions().Exit, err2 = popExitCodePArg(arg)
		}
		if err2 != nil {
			return err2
		}
	}

	if !dDash {
		return errors.New("you must specify -- followed by PROGRAM and ARGS")
	}
	tail := os.Args[argIdx+1:]
	if len(tail) < 1 {
		return errors.New("you must specify -- followed by PROGRAM and ARGS")
	}
	prg, err := exec.LookPath(tail[0])
	if err != nil {
		return err
	}
	if Opts.NoStdBuf {
		Opts.Program = prg
		if len(tail) > 1 {
			Opts.ProgramArgs = tail[1:]
		} else {
			Opts.ProgramArgs = make([]string, 0)
		}
	} else if stdbuf, err := exec.LookPath("stdbuf"); err != nil {
		// no coreutils (busybox, Alpine...): start PROGRAM directly, main() warns once
		Opts.StdBufMissing = true
		Opts.Program = prg
		Opts.ProgramArgs = append([]string{}, tail[1:]...)
	} else {
		Opts.Program = stdbuf
		Opts.ProgramArgs = append([]string{"-oL", "-eL", prg}, tail[1:]...)
	}

	return validateOptions()
}

func changeColorAttribute(attr color.Attribute) {
	if currentActions().Color == nil {
		currentActions().Color = color.New(attr)
	} else {
		currentActions().Color = currentActions().Color.Add(attr)
	}
}

func isGlobalOption(opt Option) bool {
	switch opt {
	case Help:
		return true
	case ShowVersion:
		return true
	case ListSignals:
		return true
	case PID:
		return true
	case LineBufferSize:
		return true
	case NoStdBuf:
		return true
	case NoStdIn:
		return true
	case StopSignal:
		return true
	case Deadline:
		return true
	case ShareCommands:
		return true
	case ShareStreams:
		return true
	default:
		return false
	}
}

func ParseArgs() (Type, error) {
	err := internalParseArgs()
	if err != nil {
		return Type{}, err
	}
	return Opts, nil
}

func appendNameArg(name string, i *[]string) error {
	n, err2 := popNameArg(name)
	if err2 != nil {
		return err2
	}
	*i = append(*i, n)
	return nil
}

func addEmptyCommand() {
	Opts.Commands = append(Opts.Commands, CreateCommand())
	cmdIdx = len(Opts.Commands) - 1
}

func addPattern(arg string) error {
	p, err := popStringArg(arg)
	if err != nil {
		return err
	}
	if p == "" {
		return errors.New("pattern must not be empty")
	}
	currentConditions().RawPatterns = append(currentConditions().RawPatterns, p)
	return nil
}

func currentCommand() *Command {
	return &Opts.Commands[cmdIdx]
}

func currentActions() *CommandActions {
	return currentCommand().Actions
}

func currentConditions() *CommandConditions {
	return currentCommand().Conditions
}
