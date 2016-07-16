package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"

	"github.com/alecthomas/kingpin"
	"github.com/fsnotify/fsnotify"
	"github.com/gurupras/gocommons"
)

var (
	Version        string
	CpusetBasePath = "/sys/fs/cgroup/cpuset"
	LogPath        = "/dev/kmsg"
	LogBuf         *bufio.Writer
)

const (
	TAG = "ThermaPlan"
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

	for {
		select {
		case event := <-container.Watcher.Events:
			if event.Op&fsnotify.Write == fsnotify.Write {
				log("/proc/foreground received write")
				if err = migrateTasks(fgBgTf, bgCgroupTf); err != nil {
					break
				}
			} else {
				//log("bg cgroup file received: ", event.Op)
			}
		}
	}
	defer container.File.Close()
	defer container.Watcher.Close()
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
	for {
		select {
		case event := <-container.Watcher.Events:
			if event.Op&fsnotify.Write == fsnotify.Write {
				//log("bg cgroup file received write")
				if err = migrateTasks(bgCgroupTf, bgCpusetTf); err != nil {
					break
				}
			} else {
				//log("bg cgroup file received: ", event.Op)
			}
		}
	}
	defer container.File.Close()
	defer container.Watcher.Close()
	container.NotifyChannel <- struct{}{}
}

func MpdecisionUpcallHandler(container *InotifyContainer) {
	log("Starting watcher: mpdecision")

	for {
		event := <-container.Watcher.Events
		switch event {
		default:
			log("mpdecision_coexist_upcall file received event")
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
			if mpdecisionBlocked, err := strconv.Atoi(state); err != nil {
				log(fmt.Sprintf("Could not convert '%s' to int", state))
				break
			} else {
				file := "/dev/cpuctl/bg_non_interactive/cpuset.cpus"
				if mpdecisionBlocked == 1 {
					cpus := "0"
					if err = write(file, cpus); err != nil {
						log(fmt.Sprintf("Failed to write '%s' to: %s", cpus, file))
						break
					}
				} else {
					cpus := "0-3"
					if err = write(file, cpus); err != nil {
						log(fmt.Sprintf("Failed to write 3 to: %s", cpus, file))
						break
					}
				}
			}
		}
	}
	defer container.File.Close()
	defer container.Watcher.Close()
	container.NotifyChannel <- struct{}{}
}

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
	bgNotifyContainer := new(InotifyContainer)
	bgNotifyContainer.FilePath = "/dev/cpuctl/bg_non_interactive/tasks"
	bgNotifyContainer.NotifyChannel = make(chan struct{}, 0)
	bgNotifyContainer.Handler = BgCgroupHandler
	AddWatcher(bgNotifyContainer)

	mpdecisionUpcallContainer := new(InotifyContainer)
	mpdecisionUpcallContainer.FilePath = "/sys/tempfreq/mpdecision_coexist_upcall"
	mpdecisionUpcallContainer.NotifyChannel = make(chan struct{}, 0)
	mpdecisionUpcallContainer.Handler = MpdecisionUpcallHandler
	AddWatcher(mpdecisionUpcallContainer)

	FgBgMigrationContainer := new(InotifyContainer)
	FgBgMigrationContainer.FilePath = "/proc/foreground"
	FgBgMigrationContainer.NotifyChannel = make(chan struct{}, 0)
	FgBgMigrationContainer.Handler = FgBgMigrationHandler
	AddWatcher(FgBgMigrationContainer)

	<-bgNotifyContainer.NotifyChannel
	return
}

func Main(argv []string) {
	init_kingpin()

	if _, err := app.Parse(argv[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	LogPath = *LogPathPtr

	log_file, err := os.OpenFile(LogPath, os.O_WRONLY, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to open:", LogPath)
		os.Exit(-1)
	}
	LogBuf = bufio.NewWriter(log_file)

	log(fmt.Sprintf("%s - %s", argv[0], Version))

	log("verbose:", *verbose)
	log("bg_cpu:", *bg_cpu)

	Process()
}

func main() {
	Main(os.Args)
}
