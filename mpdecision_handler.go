package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gurupras/gocommons"
)

func MpdecisionHandler(cmd *NetlinkCmd, signal chan struct{}) {
	args := strings.TrimSpace(string(cmd.Args[:]))
	switch args {
	case "0":
		// Kernel is disabling mpdecision blocking
		log("Kernel disabling mpdecision blocking")
		go UnblockMpdecision(signal)
		<-signal
		Socket.SendString("0")
	case "1":
		// Kernel is enabling mpdecision blocking
		log("Kernel enabling mpdecision blocking")
		go BlockMpdecision(signal)
		<-signal
		Socket.SendString("1")
	default:
		log(fmt.Sprintf("Unknown message from kernel: '%s'", args))
	}
}

func MpdecisionCoexistUpcallHandler(container *InotifyContainer) {
	var mpdecisionBlocked int = -1

	handleUpcall := func() {
		var err error
		file := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/cpuset.cpus"
		rootCpusetCpus := "/sys/fs/cgroup/cpuset/cpuset.cpus"

		log("Handling mpdecision upcall")

		switch mpdecisionBlocked {
		case 1:
			cpus := "0"
			if err = write(file, cpus); err != nil {
				log(fmt.Sprintf("Failed to write '%s' to: %s", cpus, file))
				break
			}
		case 0:
			cpus := "0-3"
			// First write this to the root cpuset
			if err = write(rootCpusetCpus, cpus); err != nil {
				log(fmt.Sprintf("Failed to write '%s' to: %s", cpus, rootCpusetCpus))
				break
			}

			if err = write(file, "0"); err != nil {
				log(fmt.Sprintf("Failed to write '%s' to: %s", cpus, file))
				break
			}
		default:
			log("Unknown mpdecisionBlocked state:", mpdecisionBlocked)
		}
	}

	work := func() error {
		var err error

		filePath := "/sys/tempfreq/mpdecision_coexist_upcall"
		var bytes []byte
		if bytes, err = ioutil.ReadFile(filePath); err != nil {
			log("Failed to read file:", filePath)
			return err
		} else {
			text := string(bytes[:])
			if val, err := strconv.Atoi(text); err != nil {
				log(fmt.Sprintf("Failed to convert '%s' to int", text))
				return err
			} else {
				if mpdecisionBlocked == -1 {
					mpdecisionBlocked = val
				} else if mpdecisionBlocked != val {
					// mpdecisionBlocked is not -1 (it is initialized), but its not equal to current value
					mpdecisionBlocked = val
					handleUpcall()
				}
			}
		}
		return err
	}
	ops := fsnotify.Chmod | fsnotify.Create | fsnotify.Remove | fsnotify.Rename | fsnotify.Write
	GroupRequests(container, 100*time.Millisecond, 150*time.Millisecond, ops, work)
	container.NotifyChannel <- struct{}{}
}

func BlockMpdecision(signal chan struct{}) {
	var b []byte
	var err error
	var bgCpus string
	var bgCgroupTf *gocommons.File
	var bgCpusetTf *gocommons.File
	//bgNotifyContainer := new(InotifyContainer)

	bgCpuFile := "/sys/tempfreq/mpdecision_bg_cpu"
	bgCpusetCpusFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/cpuset.cpus"
	bgCpusetMemsFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/cpuset.mems"
	bgCpusetTasksFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/tasks"
	bgCgroupTasksFile := "/dev/cpuctl/bg_non_interactive/tasks"

	if isBlocked {
		log("Attempting to block mpdecision when blocked")
		err = fmt.Errorf("Already blocked")
		goto out
	}

	isBlocked = true

	if b, err = ioutil.ReadFile(bgCpuFile); err != nil {
		log(fmt.Sprintf("Failed to read '%s': %s", bgCpuFile, err))
		return
	}
	bgCpus = string(b[:])

	if err = write(bgCpusetMemsFile, "0"); err != nil {
		log("Failed to set mems to '0':", err)
		goto out
	}
	if err = write(bgCpusetCpusFile, bgCpus); err != nil {
		log(fmt.Sprintf("Failed to set cpus to '%s':%v", bgCpus, err))
		goto out
	}

	if bgCgroupTf, err = gocommons.Open(bgCgroupTasksFile, os.O_RDONLY, gocommons.GZ_FALSE); err != nil {
		log("Could not open bg cgroup tasks file for copying to bg cpuset")
		goto out
	}
	defer bgCgroupTf.Close()

	if bgCpusetTf, err = gocommons.Open(bgCpusetTasksFile, os.O_WRONLY, gocommons.GZ_FALSE); err != nil {
		log("Could not open bg cpuset tasks file for writing")
		goto out
	}
	defer bgCpusetTf.Close()

	if err = migrateTasks(bgCgroupTf, bgCpusetTf); err != nil {
		log("Failed to migrate tasks from bg cgroup to bg cpuset")
		goto out
	}

	// We don't add a watcher since the kernel takes care of doing this
	// once we send it the signal that we've set up the cpuset
	/*
		bgNotifyContainer.FilePath = "/dev/cpuctl/bg_non_interactive/tasks"
		bgNotifyContainer.NotifyChannel = make(chan struct{}, 0)
		bgNotifyContainer.Handler = BgCgroupHandler
		AddWatcher(bgNotifyContainer)
	*/
out:
	// Signal that we're done
	signal <- struct{}{}

	if err == nil {
		// Now wait for unblock to signal us to terminate
		<-signalChan
		log("Received signal to unblock")
	}
	//bgNotifyContainer.IsDone = true
}

func UnblockMpdecision(signal chan struct{}) {
	var rootCpusetTf *gocommons.File
	var bgCpusetTf *gocommons.File
	var err error

	rootTasksFile := "/sys/fs/cgroup/cpuset/tasks"
	bgCpusetTasksFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/tasks"
	bgCpusetCpusFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/cpuset.cpus"
	bgCpusetMemsFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/cpuset.mems"

	if !isBlocked {
		log("Attempting to unblock mpdecision when not blocked")
		goto out
	}
	// Signal block to terminate
	signalChan <- struct{}{}
	log("Sent signal to unblock")
	isBlocked = false
	if err = write(bgCpusetMemsFile, ""); err != nil {
		log("Unblock: Failed to set mems to '':", err)
		goto out
	}
	if err = write(bgCpusetCpusFile, ""); err != nil {
		log(fmt.Sprintf("Unblock: Failed to set cpus to '':%v", err))
		goto out
	}

	if bgCpusetTf, err = gocommons.Open(bgCpusetTasksFile, os.O_RDONLY, gocommons.GZ_FALSE); err != nil {
		log("Could not open root bg cpuset tasks file for copying to root cpuset")
		return
	}
	defer bgCpusetTf.Close()

	if rootCpusetTf, err = gocommons.Open(rootTasksFile, os.O_WRONLY, gocommons.GZ_FALSE); err != nil {
		log("Could not open root cpuset tasks file for writing")
		return
	}
	defer rootCpusetTf.Close()

	if err = migrateTasks(bgCpusetTf, rootCpusetTf); err != nil {
		log(fmt.Sprintf("Unblock: Failed to migrate tasks:%v", err))
		goto out
	}
out:
	// Signal that we're done
	signal <- struct{}{}
}
