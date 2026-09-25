package opts

import (
	"regexp"
	"syscall"
	"time"

	"github.com/fatih/color"
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
	RawPatterns      []string
	CompiledPatterns []*regexp.Regexp
	Or               bool
	And              bool
	No               bool
	StdOut           bool
	StdErr           bool
	AndTimeout       *time.Duration
	OrTimeout        *time.Duration
	MinMatchTime     *time.Duration
	NoInputFor       *time.Duration
}

type Command struct {
	Name         string
	Disabled     bool
	LineDisabled bool
	LineEnabled  bool
	Conditions   *CommandConditions
	Actions      *CommandActions
	Started      time.Time
}

func (c *Command) ResetStarted() {
	c.Started = time.Now()
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

// Clone returns a deep copy of the command: a fresh Command struct with its own
// Conditions and Actions structs and its own copies of every slice. Compiled
// regexps and colors are shared because they are immutable once built.
func (c *Command) Clone() Command {
	cp := *c
	cond := *c.Conditions
	cond.RawPatterns = append([]string(nil), c.Conditions.RawPatterns...)
	cond.CompiledPatterns = append([]*regexp.Regexp(nil), c.Conditions.CompiledPatterns...)
	cp.Conditions = &cond
	act := *c.Actions
	act.Disable = append([]string(nil), c.Actions.Disable...)
	act.Enable = append([]string(nil), c.Actions.Enable...)
	act.Toggle = append([]string(nil), c.Actions.Toggle...)
	cp.Actions = &act
	return cp
}

// CloneCommands returns a deep copy of a command chain, so that runtime state
// changes (--disable, --enable, --toggle) on one copy do not affect the other.
func CloneCommands(commands []Command) []Command {
	out := make([]Command, len(commands))
	for i := range commands {
		out[i] = commands[i].Clone()
	}
	return out
}
