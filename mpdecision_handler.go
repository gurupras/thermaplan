package main

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

func MpdecisionHandler(cmd *NetlinkCmd) {
	args := strings.TrimSpace(string(cmd.Args[:]))
	signal := make(chan struct{}, 0)
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
	//bgNotifyContainer := new(InotifyContainer)

	bgCpuFile := "/sys/tempfreq/mpdecision_bg_cpu"
	bgCpusetCpusFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/cpuset.cpus"
	bgCpusetMemsFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/cpuset.mems"
	bgCpusetTasksFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/tasks"
	bgCgroupTasksFile := "/dev/cpuctl/bg_non_interactive/tasks"

	fgBgCgroupTasksFile := "/dev/cpuctl/fg_bg/tasks"
	fgBgCpusetTasksFile := "/sys/fs/cgroup/cpuset/cs_fg_bg/tasks"

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

	if err = migrateTasks(bgCgroupTasksFile, bgCpusetTasksFile); err != nil {
		log("Failed to migrate tasks from bg cgroup to bg cpuset")
		goto out
	}
	if err = migrateTasks(fgBgCgroupTasksFile, fgBgCpusetTasksFile); err != nil {
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
	var err error

	rootCpusetTasksFile := "/sys/fs/cgroup/cpuset/tasks"
	bgCpusetTasksFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/tasks"
	bgCpusetCpusFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/cpuset.cpus"
	bgCpusetMemsFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/cpuset.mems"
	fgBgCpusetTasksFile := "/sys/fs/cgroup/cpuset/cs_fg_bg/tasks"

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

	if err = migrateTasks(bgCpusetTasksFile, rootCpusetTasksFile); err != nil {
		log(fmt.Sprintf("Unblock: Failed to migrate tasks from bg_non_interactive to root:%v", err))
		goto out
	}
	if err = migrateTasks(fgBgCpusetTasksFile, rootCpusetTasksFile); err != nil {
		log(fmt.Sprintf("Unblock: Failed to migrate tasks from fg_bg to root:%v", err))
		goto out
	}
out:
	// Signal that we're done
	signal <- struct{}{}
}
