package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"syscall"
)

const (
	MPDECISION_COEXIST int = syscall.NETLINK_USERSOCK
)

var (
	Socket *NetlinkSocket
	SeqNum uint32 = 0
)

type SocketInterface interface {
	SendString(message string) error
	Send(b []byte) error
	Recv() ([]syscall.NetlinkMessage, error)
}

type NetlinkSocket struct {
	Fd   int
	Addr syscall.SockaddrNetlink
}

func (nl *NetlinkSocket) SendString(message string) error {
	return nl.Send([]byte(message))
}

func (nl *NetlinkSocket) Send(b []byte) error {
	var msg syscall.NetlinkMessage
	var destAddr syscall.SockaddrNetlink

	destAddr.Family = syscall.AF_NETLINK
	destAddr.Pid = 0
	destAddr.Groups = 1

	SeqNum++
	buf := bytes.NewBuffer(nil)
	msg.Header.Len = uint32(syscall.NLMSG_HDRLEN + len(b))
	msg.Header.Seq = SeqNum
	msg.Header.Pid = nl.Addr.Pid
	binary.Write(buf, binary.LittleEndian, msg.Header)
	buf.Write(b)
	//return syscall.Sendto(nl.Fd, buf.Bytes(), 0, &nl.Addr)
	log(fmt.Sprintf("Sending %d bytes", len(buf.Bytes())))
	return syscall.Sendmsg(nl.Fd, buf.Bytes(), nil, &destAddr, 0)
}

func (nl *NetlinkSocket) Recv() (messages []syscall.NetlinkMessage, err error) {
	var nr int

	b := make([]byte, syscall.Getpagesize())
	if nr, _, err = syscall.Recvfrom(nl.Fd, b, 0); err != nil {
		return nil, fmt.Errorf("Failed recvfrom():", err)
	}
	if nr < syscall.NLMSG_HDRLEN {
		return nil, fmt.Errorf(fmt.Sprintf("Short message from netlink socket received=%d", nr))
	}
	b = b[:nr]
	if messages, err = syscall.ParseNetlinkMessage(b); err != nil {
		return nil, fmt.Errorf("Failed syscall.ParseNetlinkMessag():", err)
	}
	return
}

func NewNetlinkSocket(protocol int) (nl *NetlinkSocket, err error) {
	var fd int
	nl = new(NetlinkSocket)
	if fd, err = syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, protocol); err != nil {
		fd = -1
		return
	}
	nl.Addr.Family = syscall.AF_NETLINK
	nl.Addr.Pid = uint32(syscall.Getpid())
	if err = syscall.Bind(fd, &nl.Addr); err != nil {
		syscall.Close(fd)
		fd = -1
		return
	}
	nl.Fd = fd
	return
}

func InitializeNetlinkConnection() (err error) {
	var nl *NetlinkSocket
	if nl, err = NewNetlinkSocket(MPDECISION_COEXIST); err != nil {
		log("Failed to open netlink socket:", err)
		return
	}
	Socket = nl
	for {
		if err = Socket.SendString("hello"); err != nil {
			log("Sending hello failed:", err)
			continue
		} else {
			break
		}
	}
	return
}
