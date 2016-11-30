package main

import (
	"fmt"
	"strconv"
	"strings"
)

func CpusetHandler(cmd *NetlinkCmd) {
	args := strings.TrimSpace(string(cmd.Args[:]))
	tokens := strings.Split(args, " ")
	switch len(tokens) {
	case 2:
		cpuset := tokens[0]
		pid, err := strconv.Atoi(tokens[1])
		if err != nil {
			log(fmt.Sprintf("Failed to run CpusetHandler on: '%v'", args))
			return
		}
		var path string
		switch cpuset {
		case "cs_default":
			path = "/sys/fs/cgroup/cpuset/tasks"
		default:
			path = fmt.Sprintf("/sys/fs/cgroup/cpuset/%s/tasks", cpuset)
		}
		pidStr := fmt.Sprintf("%v\n", pid)
		if err = write(path, pidStr); err != nil {
			log(fmt.Sprintf("Failed to write pid '%v' to '%v': %v", pid, path, err))
		}
	default:
		log(fmt.Sprintf("Unknown message from kernel: '%s'", args))
	}
}
