package main

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
	NetlinkRecvHandler()
}
