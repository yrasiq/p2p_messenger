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

	rUDPAddr, ok := readConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fail()
		return
	}
	writeConn, err := net.DialUDP("udp", &net.UDPAddr{IP: ip}, rUDPAddr)
	if err != nil {
		t.Error(err)
		return
	}
	defer writeConn.Close()

	printIncomCh := make(chan string, 1)
	writeCh := make(chan []byte, 2)
	lastReadCh := make(chan time.Time)

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		errRead := ReadIncome(readConn, writeConn.LocalAddr(), printIncomCh, writeCh, lastReadCh)
		if !os.IsTimeout(errRead) {
			t.Error(errRead)
		}
	}()

	time.Sleep(time.Second)
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
	timeout := time.NewTimer(time.Second)
L:
	for {
		select {
		case <-lastReadCh:
			timeout.Reset(time.Second)
		case <-timeout.C:
			err = readConn.SetReadDeadline(time.Now())
			if err != nil {
				panic(err)
			}
			break L
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
