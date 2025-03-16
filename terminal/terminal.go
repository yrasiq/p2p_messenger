package terminal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yrasiq/p2p_messenger/client"
)

func PrintPending(ctx context.Context) {
	fmt.Println("Pending...")
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			fmt.Println("\033[A\r\033[KPending OK!")
			return

		case <-time.After(time.Second):
			fmt.Printf("\033[A\r\033[K%s", fmt.Sprintf("Pending... %s\n", time.Since(start).Truncate(time.Second)))
		}
	}
}

func InputAddr(scanner *bufio.Scanner) []*net.UDPAddr {
	inputHeader := "Write address for connect: "
	fmt.Print(inputHeader)
	for {
		if !scanner.Scan() {
			fmt.Println()
			return nil
		}
		text := scanner.Text()
		if text == "" {
			fmt.Printf("\033[A%s", inputHeader)
			continue
		}
		var err error
		var addr *net.UDPAddr
		var addrs []*net.UDPAddr
		for _, addrStr := range strings.Split(text, ",") {
			addr, err = net.ResolveUDPAddr("udp", strings.Trim(addrStr, " "))
			if err != nil {
				break
			}
			addrs = append(addrs, addr)
		}
		if err != nil {
			fmt.Printf("\033[A\r\033[K%s\n%s", err, inputHeader)
			continue
		}
		return addrs
	}
}

func PrintMessages(incCh, outCh <-chan string) {
	var lastInput string
	var str string
	var ok bool
	for incCh != nil || outCh != nil {
		select {
		case str, ok = <-incCh:
			if !ok {
				incCh = nil
				continue
			}
			str = fmt.Sprintf("%s%s", str, lastInput)

		case str, ok = <-outCh:
			if !ok {
				outCh = nil
				continue
			}
			if strings.HasSuffix(str, "\r\n") {
				lastInput = ""
			} else {
				lastInput = str
			}
		}
		fmt.Printf("\r\033[K%s", str)
	}
}

func Input(output chan<- string, writeCh chan<- []byte) error {
	var input []rune
	for {
		b := make([]byte, 4)
		n, err := os.Stdin.Read(b)
		if err != nil {
			return err
		}
		if n > 4 {
			return errors.New("input error")
		}
		r, _ := utf8.DecodeRune(b[:n])
		switch r {
		case '\x03', '\x04':
			return nil

		case '\r', '\n':
			if len(input) == 0 {
				continue
			}
			msg := client.MakeMessage(string(input))
			writeCh <- msg.Bytes()
			output <- fmt.Sprintf("%s   me: %s\r\n", msg.Time.Format("2006-01-02 15:04:05"), msg.Str)
			input = []rune{}
			continue

		case '\b', 127:
			if len(input) > 0 {
				input = input[:len(input)-1]
			}

		default:
			input = append(input, r)
		}
		output <- string(input)
	}
}
