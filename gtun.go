package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/ICKelin/glog"
	"github.com/songgao/water"
)

var (
	psrv = flag.String("s", "120.25.214.63:9621", "srv address")
	pdev = flag.String("dev", "gtun", "local tun device name")
	pkey = flag.String("key", "gtun_authorize", "client authorize key")
)

func main() {
	flag.Parse()

	cfg := water.Config{
		DeviceType: water.TUN,
	}
	cfg.Name = *pdev
	ifce, err := water.New(cfg)

	if err != nil {
		glog.ERROR(err)
		return
	}

	conn, err := ConServer(*psrv)
	if err != nil {
		glog.ERROR(err)
		return
	}
	defer conn.Close()

	err = Authorize(conn, *pkey)
	if err != nil {
		glog.ERROR("authorize fail")
		return
	}

	glog.INFO("authorize success...")

	tunip, err := GetTunIP(conn)
	if err != nil {
		glog.ERROR(err)
		return
	}

	err = SetTunIP(*pdev, tunip)
	if err != nil {
		glog.ERROR(err)
		return
	}

	go IfaceRead(ifce, conn)
	go IfaceWrite(ifce, conn)

	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGABRT, syscall.SIGHUP)
	<-sig
}

func IfaceRead(ifce *water.Interface, conn net.Conn) {
	packet := make([]byte, 65536)
	for {
		n, err := ifce.Read(packet)
		if err != nil {
			glog.ERROR(err)
			break
		}

		err = ForwardSrv(conn, packet[:n])
		if err != nil {
			glog.ERROR(err)
		}
	}
}

func IfaceWrite(ifce *water.Interface, conn net.Conn) {
	plen := make([]byte, 4)
	for {
		nr, err := conn.Read(plen)
		if err != nil {
			glog.ERROR(err)
			break
		}

		payloadlength := uint32(0)
		binary.BigEndian.PutUint32(plen, payloadlength)

		if payloadlength > 65536 {
			glog.ERROR("too big ip payload")
			continue
		}

		packet := make([]byte, payloadlength)
		nr, err = conn.Read(packet)
		if err != nil {
			glog.ERROR(err)
			break
		}

		_, err = ifce.Write(packet[:nr])
		if err != nil {
			glog.ERROR(err)
		}
	}
}

func ForwardSrv(srvcon net.Conn, buff []byte) (err error) {
	output := make([]byte, 0)
	bsize := make([]byte, 4)
	binary.BigEndian.PutUint32(bsize, uint32(len(buff)))

	output = append(output, bsize...)
	output = append(output, buff...)

	left := len(output)
	for left > 0 {
		nw, er := srvcon.Write(output)
		if er != nil {
			err = er
			break
		}

		left -= nw
	}
	return err
}

func ConServer(srv string) (conn net.Conn, err error) {
	conn, err = net.Dial("tcp", srv)
	if err != nil {
		return nil, err
	}
	return conn, err
}

func GetTunIP(conn net.Conn) (tunip string, err error) {
	plen := make([]byte, 4)
	nr, err := conn.Read(plen)
	if err != nil {
		return "", err
	}

	if nr != 4 {
		return "", fmt.Errorf("too short pkt")
	}

	payloadlength := binary.BigEndian.Uint32(plen)
	buff := make([]byte, int(payloadlength))

	nr, err = conn.Read(buff)
	if err != nil {
		return "", err
	}

	return string(buff[:nr]), nil
}

func SetTunIP(dev, tunip string) (err error) {
	uptun := fmt.Sprintf("ifconfig %s up", dev)
	setip := fmt.Sprintf("ip addr add %s/24 dev %s", tunip, dev)

	err = exec.Command("/bin/sh", "-c", uptun).Run()
	if err != nil {
		return fmt.Errorf("up %s error %s", dev, err.Error())
	}

	err = exec.Command("/bin/sh", "-c", setip).Run()
	if err != nil {
		return fmt.Errorf("up %s error %s", dev, err.Error())
	}

	return nil
}

func Authorize(conn net.Conn, key string) (err error) {
	plen := make([]byte, 4)
	binary.BigEndian.PutUint32(plen, uint32(len(key)))

	buff := make([]byte, 0)
	buff = append(buff, plen...)
	buff = append(buff, []byte(key)...)

	_, err = conn.Write(buff)
	if err != nil {
		return err
	}

	status := make([]byte, 1)
	nr, err := conn.Read(status)
	if err != nil {
		return err
	}

	if nr != 1 {
		return fmt.Errorf("unexpected authorize status length")
	}

	if status[0] == 0x00 {
		return fmt.Errorf("authorize fail")
	}

	if status[0] == 0x01 {
		return nil
	}
	return fmt.Errorf("unexpected authorize status")
}
