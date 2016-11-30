package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"syscall"
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
	IsDone        bool
}

var bgCgroupHandlerStarted bool = false

func NetlinkRecvHandler() {
	var messages []syscall.NetlinkMessage
	var err error

	log("Starting NetlinkRecvHandler()")
	for {
		log("recvHandler loop")
		if messages, err = Socket.Recv(); err != nil {
			log("Failed recv:", err)
		}
		for m := range messages {
			message := messages[m]

			pos := 0
			real_len := binary.LittleEndian.Uint32(message.Data[:pos+4])
			_ = real_len
			log("Real len:", real_len)
			pos += 4

			cmdLen := binary.LittleEndian.Uint32(message.Data[pos : pos+4])
			log("cmdLen:", cmdLen)
			pos += 4

			cmdBytes := message.Data[pos : pos+NETLINK_CMD_SIZE]
			command := strings.TrimSpace(string(cmdBytes[:cmdLen]))
			log("command:", command)
			pos += NETLINK_CMD_SIZE

			argsLen := binary.LittleEndian.Uint32(message.Data[pos : pos+4])
			log("argsLen:", argsLen)
			pos += 4

			argsBytes := message.Data[pos : pos+NETLINK_ARGS_SIZE]
			args := strings.TrimSpace(string(argsBytes[:argsLen]))
			log("args:", args)
			pos += NETLINK_ARGS_SIZE

			cmd := new(NetlinkCmd)
			cmd.Cmd = command
			cmd.Args = args

			log(fmt.Sprintf("Command: %v", cmd.String()))

			switch cmd.Cmd {
			case "mpdecision":
				go MpdecisionHandler(cmd)
			case "move_to_cgroup":
				go MoveToCgroupHandler(cmd)
			case "cpuset":
				go CpusetHandler(cmd)
			default:
				log(fmt.Sprintf("Unknown command: %v", cmd.String()))
			}
		}
	}
	goto out
out:
	log("Finished NetlinkRecvHandler()")
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
	bgCpusetTasksFile := "/sys/fs/cgroup/cpuset/cs_bg_non_interactive/tasks"

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

var (
	isBlocked  = false
	signalChan = make(chan struct{}, 0)
)

func Process() (err error) {
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
	bgCpu := *bg_cpu
	write("/sys/tempfreq/mpdecision_bg_cpu", bgCpu)
	log("Informed kernel that background cpu is:", bgCpu)

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
