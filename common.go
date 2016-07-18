package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/gurupras/gocommons"
)

const (
	TAG = "ThermaPlan"
)

var (
	Version        string
	CpusetBasePath = "/sys/fs/cgroup/cpuset"
	LogPath        = "/dev/kmsg"
	LogBuf         *bufio.Writer
)

func log(msg ...interface{}) {
	LogBuf.Write([]byte(fmt.Sprintf("%v: %v\n", TAG, msg)))
	LogBuf.Flush()
}

func write(path string, text string) (err error) {
	var file *gocommons.File
	var writer gocommons.Writer

	if file, err = gocommons.Open(path, os.O_WRONLY, gocommons.GZ_FALSE); err != nil {
		return
	}
	defer file.Close()

	if writer, err = file.Writer(0); err != nil {
		return
	}
	defer writer.Close()
	defer writer.Flush()

	writer.Write([]byte(text))
	log(fmt.Sprintf("Successfully wrote '%s' to %s", text, path))
	return
}

func init_logger() {
	log_file, err := os.OpenFile(LogPath, os.O_WRONLY, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to open:", LogPath)
		os.Exit(-1)
	}
	LogBuf = bufio.NewWriter(log_file)

	log(fmt.Sprintf("%s - %s", os.Args[0], Version))

}
