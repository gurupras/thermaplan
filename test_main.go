package main

import (
	"encoding/binary"
	"fmt"
	"strings"
	"syscall"
)

func NetlinkRecvHandler() {
	var messages []syscall.NetlinkMessage
	var err error

	log("Starting NetlinkRecvHandler()")
	for {
		if messages, err = Socket.Recv(); err != nil {
			log("Failed recv:", err)
		}
		for m := range messages {
			message := messages[m]

			real_len := binary.LittleEndian.Uint32(message.Data[:4])
			//log("Real len:", real_len)
			text := strings.TrimSpace(string(message.Data[4 : 4+real_len]))
			switch text {
			default:
				log(fmt.Sprintf("Message from kernel: '%s'", text))
			}
		}
	}
	log("Finished NetlinkRecvHandler()")
}

func main() {
	var err error

	init_logger()
	log("Attempting to initialize netlink socket ...")
	if err = InitializeNetlinkConnection(); err != nil {
		log("Failed to initialize netlink socket:", err)
		return
	} else {
		log("Successfully initialized netlink socket")
	}
	/*
		if len(os.Args) == 2 {
			if err = Socket.SendString(os.Args[1]); err != nil {
				log(fmt.Sprintf("Failed to send '%s': %v", os.Args[1], err))
			} else {
				log("Successfully sent message")
			}
		}
	*/
	//	NetlinkRecvHandler()
}
