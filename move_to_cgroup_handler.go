package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func MoveToCgroupHandler(cmd *NetlinkCmd) {
	args := strings.TrimSpace(string(cmd.Args[:]))
	/* Order is:
	 * pid(int) cgroup_name(string) should_assign_cpuset(bool)
	 */
	tokens := strings.Split(args, " ")
	if len(tokens) != 3 {
		log(fmt.Sprintf("Invalid format:  Expected: move_to_cgroup PID(int) cgroup_name(string) should_assign_cpuset(bool)  Got: cgroup %s", args))
		return
	}

	pid, err := strconv.Atoi(tokens[0])
	if err != nil {
		log(fmt.Sprintf("Failed to convert '%v' to int: %v", tokens[0], err))
		return
	}

	cgroup := tokens[1]

	shouldAssignCpuset, err := strconv.ParseBool(tokens[2])
	if err != nil {
		log(fmt.Sprintf("Failed to convert '%v' to bool: %v", tokens[2], err))
		return
	}

	if err = MovePidToCgroup(pid, cgroup); err != nil {
		log(fmt.Sprintf("Failed to move tid (%v) to '%s' cgroup: %v", pid, cgroup, err))
		return
	}
	if shouldAssignCpuset {
		if err = MovePidToCpuset(pid, cgroup); err != nil {
			log(fmt.Sprintf("Failed to move tid (%v) to 'cs_%s' cpuset: %v", pid, cgroup, err))
			return
		}
	}
}

func MovePidToCgroup(pid int, cgroup string) error {
	cgroupTasksPath := fmt.Sprintf("/dev/cpuctl/%s/tasks", cgroup)
	return write(cgroupTasksPath, pid)
}

func MovePidToCpuset(pid int, cgroup string) error {
	cpuset := fmt.Sprintf("cs_%s", cgroup)
	cpusetTasksPath := filepath.Join(CpusetBasePath, cpuset, "tasks")
	return write(cpusetTasksPath, pid)
}
