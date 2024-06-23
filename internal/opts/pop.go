package opts

import (
	"fmt"
	"github.com/fatih/color"
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func popStringArg(name string) (string, error) {
	argIdx += 1
	if argIdx >= len(os.Args) {
		return "", fmt.Errorf("missing value for %v", name)
	}
	return os.Args[argIdx], nil
}

func popStringPArg(name string) (*string, error) {
	s, err := popStringArg(name)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func popOptName(name string) (string, error) {
	if argIdx+1 >= len(os.Args) {
		return "", nil
	}
	n := os.Args[argIdx+1]
	if n == "" {
		return "", fmt.Errorf("name of %v cannot be empty", name)
	}
	if strings.HasPrefix(n, "-") {
		return "", nil
	}
	argIdx++
	return n, nil
}

func popNameArg(name string) (string, error) {
	s, err := popStringArg(name)
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", fmt.Errorf("name for %v cannot be empty", name)
	}
	if strings.HasPrefix(s, "-") {
		return "", fmt.Errorf("name for %v cannot start with '-'", name)
	}
	return s, nil
}

func popNamePArg(name string) (*string, error) {
	s, err := popNameArg(name)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func popIntArg(name string) (int, error) {
	s, err := popStringArg(name)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("value of %v must be an int", name)
	}
	return value, nil
}

func popIntPArg(name string) (*int, error) {
	s, err := popIntArg(name)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func popSignalPArg(name string) (*syscall.Signal, error) {
	s, err := popStringArg(name)
	if err != nil {
		return nil, err
	}
	signal := unix.SignalNum(strings.ToUpper(s))
	if signal != 0 {
		return &signal, nil
	}
	no, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("value of %v must be a signal name or a signal number", name)
	}
	signal = syscall.Signal(no)
	if unix.SignalName(signal) == "" {
		return nil, fmt.Errorf("invalid signal number %v for %v", no, name)
	}
	return &signal, nil
}

func popDurationArg(name string) (*time.Duration, error) {
	timeout, err := popStringArg(name)
	if err != nil {
		return nil, err
	}
	result, err := time.ParseDuration(timeout)
	if err != nil {
		return nil, fmt.Errorf("%v: %v", name, err.Error())
	}
	return &result, nil
}

func popColorFgAttrArg(name string) (color.Attribute, error) {
	cname, err := popStringArg(name)
	if err != nil {
		return color.FgWhite, err
	}
	cname = strings.ToLower(cname)
	if cname == "black" {
		return color.FgBlack, nil
	} else if cname == "red" {
		return color.FgRed, nil
	} else if cname == "green" {
		return color.FgGreen, nil
	} else if cname == "yellow" {
		return color.FgYellow, nil
	} else if cname == "blue" {
		return color.FgBlue, nil
	} else if cname == "magenta" {
		return color.FgMagenta, nil
	} else if cname == "cyan" {
		return color.FgCyan, nil
	} else if cname == "white" {
		return color.FgWhite, nil
	} else if cname == "hi-black" {
		return color.FgHiBlack, nil
	} else if cname == "hi-red" {
		return color.FgHiRed, nil
	} else if cname == "hi-green" {
		return color.FgHiGreen, nil
	} else if cname == "hi-yellow" {
		return color.FgHiYellow, nil
	} else if cname == "hi-blue" {
		return color.FgHiBlue, nil
	} else if cname == "hi-magenta" {
		return color.FgHiMagenta, nil
	} else if cname == "hi-cyan" {
		return color.FgHiCyan, nil
	} else if cname == "hi-white" {
		return color.FgHiWhite, nil
	} else {
		return color.FgWhite, fmt.Errorf("%v: invalid color name: %s", name, cname)
	}
}

func popColorBgAttrArg(name string) (color.Attribute, error) {
	cname, err := popStringArg(name)
	if err != nil {
		return color.BgBlack, err
	}
	cname = strings.ToLower(cname)
	if cname == "black" {
		return color.BgBlack, nil
	} else if cname == "red" {
		return color.BgRed, nil
	} else if cname == "green" {
		return color.BgGreen, nil
	} else if cname == "yellow" {
		return color.BgYellow, nil
	} else if cname == "blue" {
		return color.BgBlue, nil
	} else if cname == "magenta" {
		return color.BgMagenta, nil
	} else if cname == "cyan" {
		return color.BgCyan, nil
	} else if cname == "white" {
		return color.BgWhite, nil
	} else if cname == "hi-black" {
		return color.BgBlack, nil
	} else if cname == "hi-red" {
		return color.BgHiRed, nil
	} else if cname == "hi-green" {
		return color.BgHiGreen, nil
	} else if cname == "hi-yellow" {
		return color.BgHiYellow, nil
	} else if cname == "hi-blue" {
		return color.BgHiBlue, nil
	} else if cname == "hi-magenta" {
		return color.BgHiMagenta, nil
	} else if cname == "hi-cyan" {
		return color.BgHiCyan, nil
	} else if cname == "hi-white" {
		return color.BgHiWhite, nil
	} else {
		return color.BgWhite, fmt.Errorf("%v: invalid color name: %s", name, cname)
	}
}
