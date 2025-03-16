package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yrasiq/p2p_messenger/client"
	"github.com/yrasiq/p2p_messenger/terminal"
	"github.com/yrasiq/udp_holepunch/stun"
	"golang.org/x/term"
)

const defaultStunServerAddr = "stun.sipnet.ru:3478"
const timeout = time.Second * 10

func main() {
	localAddr := flag.String("l", "", "local address")
	stunServerAddr := flag.String("s", defaultStunServerAddr, "stun server adress")
	flag.Parse()

	rAddr, err := net.ResolveUDPAddr("udp", *stunServerAddr)
	if err != nil {
		fmt.Println(err)
		return
	}

	lAddr, err := net.ResolveUDPAddr("udp", *localAddr)
	if err != nil {
		fmt.Println(err)
		return
	}

	conn, err := net.ListenUDP("udp", lAddr)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer conn.Close()

	err = conn.SetDeadline(time.Now().Add(time.Second * 5))
	if err != nil {
		fmt.Println(err)
		return
	}

	stunClient := stun.MakeClient(conn)
	myPublicAddr, err := stunClient.BindUDP(rAddr)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("You address: \n\tpublic:  %s\n", myPublicAddr)
	localIPs, err := stun.GetLocalIPs()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, ip := range localIPs {
		if ip.IsLoopback() {
			continue
		}
		if !ip.IsPrivate() {
			continue
		}
		fmt.Printf("\tprivate: %s:%d\n", ip.String(), myPublicAddr.Port)
	}

	err = conn.SetDeadline(time.Time{})
	if err != nil {
		fmt.Println(err)
		return
	}

	ctx := context.Background()
	keepAliveCtx, keepAliveCancel := context.WithCancel(ctx)
	keepAliveDone := make(chan struct {
		addr *net.UDPAddr
		err  error
	})
	go func() {
		defer close(keepAliveDone)
		keepAliveAddr, keepAliveErr := stunClient.KeepAlive(keepAliveCtx, myPublicAddr, rAddr)
		keepAliveDone <- struct {
			addr *net.UDPAddr
			err  error
		}{keepAliveAddr, keepAliveErr}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	contactAddrs := terminal.InputAddr(scanner)

	keepAliveCancel()
	switch res := <-keepAliveDone; {
	case res.err != nil:
		fmt.Println(err)
		return

	case res.addr.String() != myPublicAddr.String():
		fmt.Printf("You public address change %s -> %s\n", myPublicAddr, res.addr)
		return
	}

	if contactAddrs == nil {
		return
	}

	ctxPrintPending, cancelPrintPending := context.WithCancel(ctx)
	donePrintPending := make(chan struct{})
	go func() {
		defer close(donePrintPending)
		terminal.PrintPending(ctxPrintPending)
	}()

	contactAddr, err := client.Connect(conn, contactAddrs)
	cancelPrintPending()
	<-donePrintPending
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		errDisc := client.Disconnect(conn, contactAddr)
		if errDisc != nil {
			fmt.Println(errDisc)
		}
	}()
	fmt.Printf("Connected with: %s\n", contactAddr.String())

	writeCh := make(chan []byte)
	printIncomCh := make(chan string)
	printInputCh := make(chan string)
	printDone := make(chan struct{}, 1)
	inputDone := make(chan error, 1)
	readDone := make(chan error, 1)
	writeDone := make(chan error, 1)
	lastReadTimeCh := make(chan time.Time)

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		errRestore := term.Restore(int(os.Stdin.Fd()), state)
		if errRestore != nil {
			fmt.Println(errRestore)
		}
	}()

	go func() {
		terminal.PrintMessages(printIncomCh, printInputCh)
		close(printDone)
	}()
	go func() {
		inputDone <- terminal.Input(printInputCh, writeCh)
	}()
	go func() {
		readDone <- client.ReadIncome(conn, contactAddr, printIncomCh, writeCh, lastReadTimeCh)
	}()
	go func() {
		writeDone <- client.WriteOut(ctx, conn, contactAddr, writeCh)
	}()

	timeoutTimer := time.NewTimer(timeout)
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer func() {
		if err != nil {
			fmt.Println(err)
		}
	}()
	for {
		select {
		case <-done:
			return

		case <-timeoutTimer.C:
			fmt.Println("connection timeout")
			return

		case <-lastReadTimeCh:
			timeoutTimer.Reset(timeout)

		case <-printDone:
			return

		case err = <-inputDone:
			return

		case err = <-readDone:
			if err == nil {
				fmt.Println("contact disconnected")
			}
			return

		case err = <-writeDone:
			return
		}
	}
}
