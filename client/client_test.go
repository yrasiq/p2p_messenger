package client

import (
	"net"
	"os"
	"testing"
	"time"
)

func TestReadIncome(t *testing.T) {
	ip := net.IP{127, 0, 0, 1}
	readConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip})
	if err != nil {
		t.Error(err)
		return
	}
	defer readConn.Close()

	writeConn, err := net.DialUDP("udp", &net.UDPAddr{IP: ip}, readConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Error(err)
		return
	}
	defer writeConn.Close()

	printIncomCh := make(chan string, 1)
	writeCh := make(chan []byte, 2)

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		readConn.SetReadDeadline(time.Now().Add(time.Second * 5))
		errRead := ReadIncome(readConn, writeConn.LocalAddr(), printIncomCh, writeCh)
		if !os.IsTimeout(errRead) {
			t.Error(errRead)
		}
	}()

	for _, b := range [][]byte{
		connectRequest(makeId()).Bytes(),
		MakeMessage("hello").Bytes(),
	} {
		n, errWrite := writeConn.Write(b)
		if errWrite != nil {
			t.Error(errWrite)
		}
		if n != len(b) {
			t.Error("error write")
		}
	}
	<-readDone
	if len(printIncomCh) != 1 {
		t.Fail()
	}
	if len(writeCh) != 2 {
		t.Fail()
	}
}
