package main

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gurupras/gocommons"
)

const (
	TAG = "ThermaPlan"
)

var (
	Version        string
	Timestamp      string
	CpusetBasePath = "/sys/fs/cgroup/cpuset"
	LogPath        = "/dev/kmsg"
	LogBuf         *bufio.Writer
)

func log(msg ...interface{}) {
	LogBuf.Write([]byte(fmt.Sprintf("%v: %v\n", TAG, msg)))
	LogBuf.Flush()
}

func write(path string, data interface{}) (err error) {
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

	text := fmt.Sprintf("%v", data)
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

	log(fmt.Sprintf("%s - %s (%s)", os.Args[0], Version, Timestamp))

}

func migrateTasks(inputFile, outputFile *gocommons.File) (err error) {
	var tmpInputFile *gocommons.File
	var reader *bufio.Scanner
	var writer gocommons.Writer

	if tmpInputFile, err = gocommons.Open(inputFile.Path, os.O_RDONLY, gocommons.GZ_FALSE); err != nil {
		log("Could not open bg cgroup tasks file for copying to bg cpuset")
		return
	}
	defer tmpInputFile.Close()

	if _, err = tmpInputFile.Seek(0, 0); err != nil {
		log("Failed to seek on:", tmpInputFile.Path)
	}

	if reader, err = tmpInputFile.Reader(0); err != nil {
		log("Could not get reader to inputFile")
		return
	}
	if writer, err = outputFile.Writer(0); err != nil {
		log("Could not get writer to bg cpuset tasks file")
		return
	}
	defer writer.Flush()

	reader.Split(bufio.ScanLines)
	numLines := 0
	for reader.Scan() {
		numLines++
		pid := reader.Text()
		if _, err = writer.Write([]byte(pid)); err != nil {
			log(fmt.Sprintf("Failed to write '%s' > %s", pid, outputFile.Path))
			return
		}
		writer.Flush()
	}
	log(fmt.Sprintf("cat %s > %s (Wrote: %d lines)", tmpInputFile.Path, outputFile.Path, numLines))
	return
}

func GroupRequests(container *InotifyContainer, pollPeriod time.Duration, groupPeriod time.Duration, fsnotifyEventsMask fsnotify.Op, work func() error) {
	if container.File != nil {
		defer container.File.Close()
	}
	defer container.Watcher.Close()

	var err error
	workChan := make(chan struct{}, 100000)
	defer close(workChan)

	var done bool = false
	go func() {
		mergedChan := make(chan string, 100000)
		pollerChan := make(chan struct{}, 0)
		defer close(mergedChan)
		defer close(pollerChan)

		poller := func(controlChan chan struct{}) {
			for {
				if done {
					break
				}
				select {
				case <-controlChan:
					break
				default:
					mergedChan <- "poll"
					time.Sleep(pollPeriod)
				}
			}
		}

		runningPoller := false
		var lastTime int64 = 0
		period := int64(150 * 000 * 000)
		// WorkChan handler
		go func() {
			var lastWorkTime int64 = 0
			var period int64 = 150 * 1000 * 1000
			for {
				if done {
					break
				}
				if _, ok := <-workChan; !ok {
					pollerChan <- struct{}{}
					break
				} else {
					now := time.Now().UnixNano()
					if now-lastWorkTime >= period {
						mergedChan <- "work"
						lastWorkTime = now
					}
				}
			}
		}()
		for {
			if done {
				break
			}
			if data, ok := <-mergedChan; !ok {
				log("Breaking widowMaker routine")
				break
			} else {
				switch data {
				case "work":
					if !runningPoller {
						// Inform poller to start
						runningPoller = true
						go poller(pollerChan)
					}
					lastTime = time.Now().UnixNano()
				case "poll":
					now := time.Now().UnixNano()
					if now-lastTime > period {
						if err = work(); err != nil {
							break
						}
						// Stop poller
						runningPoller = false
						pollerChan <- struct{}{}
					}
				default:
					log("Unknown command:", data)
					break
				}
			}
		}
	}()
	for {
		if container.IsDone {
			break
		}
		event := <-container.Watcher.Events
		if event.Op&fsnotifyEventsMask != 0 {
			//log("bg cgroup file received write")
			workChan <- struct{}{}
		} else {
			//log("bg cgroup file received: ", event.Op)
		}
	}
	done = true
	log("Finished GroupRequests()")
}
