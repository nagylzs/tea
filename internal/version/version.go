package version

import (
	"fmt"
	"time"
)

var Branch string = "unset"

var Commit string = "unset"

var Built string = "unset"

func BuiltTime() *time.Time {
	res, err := time.Parse(time.RFC3339, Built)
	if err != nil {
		return nil
	}
	return &res
}

func LocalVersion() string {
	return Built + " " + Branch + " " + Commit
}

func PrintVersion() {
	fmt.Println(LocalVersion())
}
