package opts

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"
)

func validateOptions() error {
	if Opts.ShareStreams && Opts.ShareCommands {
		return errors.New("cannot combine --share-streams with --share-commands")
	}

	if len(Opts.Commands) == 0 {
		return errors.New("you must specify at least one command with -c or --command")
	}

	if Opts.PidFile != "" {
		if _, err := os.Stat(Opts.PidFile); err == nil {
			return fmt.Errorf("pid file %s already exists", Opts.PidFile)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("pid file %s: %v", Opts.PidFile, err.Error())
		}
	}

	if Opts.LineBufferSize < 1024 {
		return errors.New("--line-buffer-size must be at least 1024")
	}

	Opts.CmdIdx = make(map[string]int)
	for i, cmd := range Opts.Commands {
		if cmd.Name != "" {
			_, exists := Opts.CmdIdx[cmd.Name]
			if exists {
				return fmt.Errorf("duplicate command name %v", cmd.Name)
			}
			Opts.CmdIdx[cmd.Name] = i
		}
	}

	for i, cmd := range Opts.Commands {
		err := validateCommand(i)
		if err != nil {
			if cmd.Name == "" {
				return fmt.Errorf("command #%v: %v", i+1, err.Error())
			} else {
				return fmt.Errorf("command #%v (name=%v): %v", i+1, cmd.Name, err.Error())
			}
		}
	}

	return nil
}

func nTrue(b ...bool) int {
	n := 0
	for _, v := range b {
		if v {
			n++
		}
	}
	return n
}

func validateCommand(cmdIdx int) error {
	cmd := &Opts.Commands[cmdIdx]

	if nTrue(cmd.Disabled, cmd.LineDisabled, cmd.LineEnabled) > 1 {
		return errors.New("you can only use one of --disabled, --line-disabled, --line-enabled in a single command")
	}

	if Opts.ShareStreams {
		if cmd.Actions.MarkStdErr != nil {
			return errors.New("cannot combine --share-streams with --mark-stderr")
		}
		if cmd.Conditions.StdErr {
			return errors.New("cannot combine --share-streams with --std-err or --std-all")
		}
	}

	c := cmd.Conditions
	c.CompiledPatterns = make([]*regexp.Regexp, 0)
	for _, pat := range cmd.Conditions.RawPatterns {
		r, err := regexp.Compile(pat)
		if err != nil {
			return err
		}
		c.CompiledPatterns = append(c.CompiledPatterns, r)
	}

	if c.Or && len(c.CompiledPatterns) == 0 {
		return errors.New("it is an error to specify --or without giving at least one --pattern")
	}

	if nNonNullDurations(c.AndTimeout, c.OrTimeout, c.MinMatchTime, c.NoInputForDuration) > 1 {
		return errors.New("only a single timeout based condition can be given for a command")
	}

	a := cmd.Actions

	if a.SendToStdOut && a.SendToStdErr {
		return errors.New("--send-to-stdout and --send-to-stderr cannot be combined")
	}

	if a.SetExitCode != nil && a.ClearExitCode {
		return errors.New("--set-exit-code and --clear-exit-code cannot be combined")
	}

	err := checkNameRefs(a.Disable, a.Enable, "--disable", "--enable")
	if err != nil {
		return err
	}
	err = checkNameRefs(a.Disable, a.Toggle, "--disable", "--toggle")
	if err != nil {
		return err
	}
	err = checkNameRefs(a.Enable, a.Toggle, "--enable", "--toggle")
	if err != nil {
		return err
	}

	if a.SkipTo != nil {
		i, exists := Opts.CmdIdx[*a.SkipTo]
		if !exists {
			return fmt.Errorf("--skip-to: cannot find command with name %v", *a.SkipTo)
		}
		if i < cmdIdx {
			return fmt.Errorf("--skip-to: cannot skip to previous command %v", *a.SkipTo)
		}
	}

	return nil
}

func nNonNullDurations(values ...*time.Duration) int {
	cnt := 0
	for _, value := range values {
		if value != nil {
			cnt++
		}
	}
	return cnt
}

func checkNameRefs(refs []string, dupNames []string, n1 string, n2 string) error {
	for _, name := range refs {
		_, exists := Opts.CmdIdx[name]
		if !exists {
			return fmt.Errorf("%v: cannot find command with name %v", n1, name)
		}
		for i := range dupNames {
			if dupNames[i] == name {
				return fmt.Errorf("cannot use the name %v in both %v and %v", name, n1, n2)
			}
		}
	}
	return nil
}
