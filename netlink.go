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

type NetlinkPacket struct {
	NlMsgHdr syscall.NlMsghdr
	Magic    string
	Length   uint32
	Data     *[]byte
}

type NetlinkCmd struct {
	Cmd  string
	Args string
}

func (cmd *NetlinkCmd) String() string {
	return fmt.Sprintf("%v:%v", cmd.Cmd, cmd.Args)
}

func NewNetlinkPacket() (pkt *NetlinkPacket) {
	pkt = new(NetlinkPacket)
	pkt.Magic = "@"
	return
}

func (n *NetlinkPacket) Bytes() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, n.NlMsgHdr)
	log("After header:", buf.Len())

	buf.Write([]byte(n.Magic))
	log("After magic:", buf.Len())

	rlBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(rlBytes, n.Length)
	buf.Write(rlBytes)
	log("After len:", buf.Len())

	buf.Write(*n.Data)
	log("After data:", buf.Len())

	return buf.Bytes()
}

func (n *NetlinkPacket) UpdateDataLength(dataLen uint32) {
	n.NlMsgHdr.Len = uint32(syscall.NLMSG_HDRLEN + 1 + 4 + dataLen)
	n.Length = dataLen
}

type NetlinkSocket struct {
	Fd   int
	Addr syscall.SockaddrNetlink
}

func (nl *NetlinkSocket) SendString(message string) error {
	return nl.Send([]byte(message))
}

func (nl *NetlinkSocket) Send(b []byte) error {
	var destAddr syscall.SockaddrNetlink

	pkt := NewNetlinkPacket()

	realLength := uint32(len(b))

	destAddr.Family = syscall.AF_NETLINK
	destAddr.Pid = 0
	destAddr.Groups = 1

	SeqNum++
	pkt.UpdateDataLength(realLength)
	pkt.NlMsgHdr.Seq = SeqNum
	pkt.NlMsgHdr.Pid = nl.Addr.Pid

	pkt.Data = &b
	pktBytes := pkt.Bytes()

	log(fmt.Sprintf("Sending %d bytes", len(pktBytes)))
	return syscall.Sendmsg(nl.Fd, pktBytes, nil, &destAddr, 0)
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
