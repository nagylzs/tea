package opts

import (
	"github.com/fatih/color"
	"regexp"
	"syscall"
	"time"
)

type CommandActions struct {
	MarkStdOut    *string
	MarkStdErr    *string
	SetPrefix     *string
	SetSuffix     *string
	NextLine      bool
	SkipTo        *string
	Disable       []string
	Enable        []string
	Toggle        []string
	Signal        *syscall.Signal
	Input         *string
	InputFile     *string
	CloseStdIn    bool
	SetExitCode   *int32
	ClearExitCode bool
	SendToStdOut  bool
	SendToStdErr  bool
	Color         *color.Color
}

type CommandConditions struct {
	RawPatterns        []string
	CompiledPatterns   []*regexp.Regexp
	Or                 bool
	And                bool
	No                 bool
	StdOut             bool
	StdErr             bool
	AndTimeout         *time.Duration
	OrTimeout          *time.Duration
	MinMatchTime       *time.Duration
	NoInputForDuration *time.Duration
}

type Command struct {
	Name         string
	Disabled     bool
	LineDisabled bool
	LineEnabled  bool
	Conditions   *CommandConditions
	Actions      *CommandActions
}

func CreateCommand() Command {
	return Command{Conditions: CreateConditions(), Actions: CreateActions()}
}

func CreateActions() *CommandActions {
	return &CommandActions{Disable: make([]string, 0), Enable: make([]string, 0), Toggle: make([]string, 0)}
}

func CreateConditions() *CommandConditions {
	return &CommandConditions{RawPatterns: make([]string, 0), StdOut: true}
}
