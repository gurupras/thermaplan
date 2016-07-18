package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/fsnotify/fsnotify"
	"github.com/gurupras/gocommons"
)

var (
	app        *kingpin.Application
	verbose    *bool
	LogPathPtr *string
	bg_cpu     *string
)

func init_kingpin() {
	app = kingpin.New("thermaplan", "Userspace module to manage temperature")
	verbose = app.Flag("verbose", "Enable verbose output").Short('v').Default("false").Bool()
	bg_cpu = app.Flag("bg_cpu", "Background cpu").Short('b').Default("0").String()
	LogPathPtr = app.Flag("log_path", "Log path").Short('l').Default(LogPath).String()
}

type FsNotifyHandler func(Container *InotifyContainer)

type InotifyContainer struct {
	Watcher       *fsnotify.Watcher
	FilePath      string
	File          *gocommons.File
	Handler       FsNotifyHandler
	NotifyChannel chan struct{}
}

var bgCgroupHandlerStarted bool = false

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
	}
	log(fmt.Sprintf("cat %s > %s (Wrote: %d lines)", tmpInputFile.Path, outputFile.Path, numLines))
	return
}

func GroupRequests(container *InotifyContainer, pollPeriod time.Duration, groupPeriod time.Duration, fsnotifyEventsMask fsnotify.Op, work func() error) {
	defer container.File.Close()
	defer container.Watcher.Close()

	var err error
	workChan := make(chan struct{}, 100)

	go func() {
		pollerChan := make(chan struct{}, 0)
		mergedChan := make(chan string, 0)

		poller := func(controlChan chan struct{}) {
			for {
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
			var period int64 = 50 * 1000 * 1000
			for {
				if _, ok := <-workChan; !ok {
					break
				} else {
					now := time.Now().UnixNano()
					if now-lastWorkTime >= period {
						mergedChan <- "work"
					}
				}
			}
		}()
		for {
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
		event := <-container.Watcher.Events
		if event.Op&fsnotifyEventsMask != 0 {
			//log("bg cgroup file received write")
			workChan <- struct{}{}
		} else {
			//log("bg cgroup file received: ", event.Op)
		}
	}
}

func FgBgMigrationHandler(container *InotifyContainer) {
	fgBgTfPath := "/dev/cpuctl/fg_bg/tasks"
	bgCgroupTfPath := "/dev/cpuctl/bg_non_interactive/tasks"

	log("Starting watcher: FgBgMigration")

	var fgBgTf *gocommons.File
	var bgCgroupTf *gocommons.File
	var err error

	if fgBgTf, err = gocommons.Open(fgBgTfPath, os.O_RDONLY, gocommons.GZ_FALSE); err != nil {
		log("Could not open bg cgroup tasks file for copying to bg cpuset")
		return
	}
	defer fgBgTf.Close()

	if bgCgroupTf, err = gocommons.Open(bgCgroupTfPath, os.O_WRONLY, gocommons.GZ_FALSE); err != nil {
		log("Could not open bg cpuset tasks file for writing")
		return
	}
	defer bgCgroupTf.Close()

	work := func() error {
		return migrateTasks(fgBgTf, bgCgroupTf)
	}
	GroupRequests(container, 100*time.Millisecond, 150*time.Millisecond, fsnotify.Write, work)

	container.NotifyChannel <- struct{}{}
}

func BgCgroupHandler(container *InotifyContainer) {
	bgCpusetTasksFile := "/sys/fs/cgroup/cpuset/bg_non_interactive/tasks"

	if bgCgroupHandlerStarted {
		return
	}
	bgCgroupHandlerStarted = true
	log("Starting watcher: bgCgroup")

	var bgCgroupTf *gocommons.File
	var bgCpusetTf *gocommons.File
	var err error

	if bgCgroupTf, err = gocommons.Open(container.FilePath, os.O_RDONLY, gocommons.GZ_FALSE); err != nil {
		log("Could not open bg cgroup tasks file for copying to bg cpuset")
		return
	}
	defer bgCgroupTf.Close()

	if bgCpusetTf, err = gocommons.Open(bgCpusetTasksFile, os.O_WRONLY, gocommons.GZ_FALSE); err != nil {
		log("Could not open bg cpuset tasks file for writing")
		return
	}
	defer bgCpusetTf.Close()

	work := func() error {
		return migrateTasks(bgCgroupTf, bgCpusetTf)
	}

	GroupRequests(container, 100*time.Millisecond, 150*time.Millisecond, fsnotify.Write, work)
	container.NotifyChannel <- struct{}{}
}

func MpdecisionCoexistUpcallHandler(container *InotifyContainer) {
	var mpdecisionBlocked int = -1

	handleUpcall := func() {
		var err error
		file := "/dev/cpuctl/bg_non_interactive/cpuset.cpus"
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

/*
func MpdecisionUpcallHandler(container *InotifyContainer) {
	log("Starting watcher: mpdecision")
	var mpdecisionBlocked int = -1

		poll := func() {
			var file *gocommons.File
			var reader *bufio.Scanner
			var err error

			if file, err = gocommons.Open(container.FilePath, os.O_RDONLY, gocommons.GZ_FALSE); err != nil {
				log("Could not open bg cgroup tasks file for copying to bg cpuset")
				break
			}
			defer file.Close()
			if reader, err = file.Reader(0); err != nil {
				log("Could not get reader to bg cgroup tasks file")
				break
			}
			var state string
			reader.Split(bufio.ScanLines)
			for reader.Scan() {
				state = reader.Text()
			}
			if mpdecisionBlocked, err = strconv.Atoi(state); err != nil {
				log(fmt.Sprintf("Could not convert '%s' to int", state))
				break
			} else {
				handleUpcall(mpdecisionBlocked)
			}
		}

	handleUpcall := func() {
		var err error
		file := "/dev/cpuctl/bg_non_interactive/cpuset.cpus"

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
			if err = write(file, cpus); err != nil {
				log(fmt.Sprintf("Failed to write 3 to: %s", cpus, file))
				break
			}
		default:
			log("Unknown mpdecisionBlocked state:", mpdecisionBlocked)
		}
	}


		for {
			event := <-container.Watcher.Events
			switch event {
			default:
				log("mpdecision_coexist_upcall file received event")
				handleUpcall()
			}
		}
	defer container.File.Close()
	defer container.Watcher.Close()
	container.NotifyChannel <- struct{}
}
*/

func AddWatcher(container *InotifyContainer) (err error) {
	log("Setting up watcher")

	if container.Watcher, err = fsnotify.NewWatcher(); err != nil {
		log("Could not create fsnotify.Watcher()")
		return
	}

	if err = container.Watcher.Add(container.FilePath); err != nil {
		log("Could not add watcher to:", container.FilePath)
		return
	} else {
		go container.Handler(container)
		log("Successfully added watcher to:", container.FilePath)

	}
	return
}

func Process() (err error) {
	/*
		bgNotifyContainer := new(InotifyContainer)
		bgNotifyContainer.FilePath = "/dev/cpuctl/bg_non_interactive/tasks"
		bgNotifyContainer.NotifyChannel = make(chan struct{}, 0)
		bgNotifyContainer.Handler = BgCgroupHandler
		AddWatcher(bgNotifyContainer)
	*/
	/*
		// XXX: Currently, mpdecision upcall handler does not work as expected
		// sysfs_notify is not making its way to fsnotify
		mpdecisionUpcallContainer := new(InotifyContainer)
		mpdecisionUpcallContainer.FilePath = "/sys/tempfreq/mpdecision_coexist_upcall"
		mpdecisionUpcallContainer.NotifyChannel = make(chan struct{}, 0)
		mpdecisionUpcallContainer.Handler = MpdecisionCoexistUpcallHandler
		AddWatcher(mpdecisionUpcallContainer)
	*/
	/*
		FgBgMigrationContainer := new(InotifyContainer)
		FgBgMigrationContainer.FilePath = "/proc/foreground"
		FgBgMigrationContainer.NotifyChannel = make(chan struct{}, 0)
		FgBgMigrationContainer.Handler = FgBgMigrationHandler
		AddWatcher(FgBgMigrationContainer)
	*/
	if err = InitializeNetlinkConnection(); err != nil {
		return
	}
	go NetlinkRecvHandler()
	//go MpdecisionCoexistHandler()

	tmp := make(chan struct{}, 0)
	<-tmp
	return
}

func Main(argv []string) {
	init_kingpin()

	if _, err := app.Parse(argv[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	LogPath = *LogPathPtr

	init_logger()

	log("verbose:", *verbose)
	log("bg_cpu:", *bg_cpu)

	Process()
}

func main() {
	Main(os.Args)
}
