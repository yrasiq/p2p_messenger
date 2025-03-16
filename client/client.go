package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/yrasiq/udp_holepunch/log"
)

const (
	connReq   = 0x01
	connRes   = 0x02
	msgReq    = 0x03
	msgRes    = 0x04
	connClose = 0x05
)

type id [16]byte

func makeId() id {
	var id_ id
	if _, err := rand.Read(id_[:]); err != nil {
		panic(err)
	}
	return id_
}

type connectRequest id

func (cr connectRequest) Bytes() []byte {
	b := make([]byte, 17)
	b[0] = connReq
	copy(b[1:], cr[:])
	return b
}

type connectResponse id

func (cr connectResponse) Bytes() []byte {
	b := make([]byte, 17)
	b[0] = connRes
	copy(b[1:], cr[:])
	return b
}

type connectClose id

func (cc connectClose) Bytes() []byte {
	b := make([]byte, 17)
	b[0] = connClose
	copy(b[1:], cc[:])
	return b
}

type headers struct {
	type_ uint8
	id    id
}

func fromBytesHeaders(b []byte) (*headers, error) {
	if len(b) != 17 {
		return nil, errors.New("invalid data")
	}
	return &headers{
		type_: b[0],
		id:    id(b[1:]),
	}, nil
}

type message struct {
	Time time.Time
	Str  string
}

func MakeMessage(str string) message {
	return message{
		Time: time.Now(),
		Str:  str,
	}
}

func fromBytesMessage(b []byte) (*message, error) {
	if len(b) < 8 {
		return nil, errors.New("invalid data")
	}
	var msg message
	msg.Time = time.Unix(int64(binary.BigEndian.Uint64(b[:8])), 0)
	msg.Str = string(b[8:])
	return &msg, nil
}

func (m message) Bytes() []byte {
	b := make([]byte, 25+len(m.Str))
	b[0] = msgReq
	id_ := makeId()
	copy(b[1:17], id_[:])
	binary.BigEndian.PutUint64(b[17:25], uint64(m.Time.Unix()))
	copy(b[25:], m.Str)
	return b
}

type messageResponse id

func (mr messageResponse) Bytes() []byte {
	b := make([]byte, 17)
	b[0] = msgRes
	copy(b[1:], mr[:])
	return b
}

func Connect(conn *net.UDPConn, rAddrs []*net.UDPAddr) (*net.UDPAddr, error) {
	if len(rAddrs) == 0 {
		return nil, errors.New("empty addrs")
	}
	rAddrsMap := make(map[string]struct{}, len(rAddrs))
	for _, a := range rAddrs {
		rAddrsMap[a.String()] = struct{}{}
	}
	req := connectRequest(makeId())
	writeCh := make(chan connectResponse)

	var errRead error
	var connAddr *net.UDPAddr
	go func() {
		defer close(writeCh)
		rcvBuff := make([]byte, 1024)
		resSended := false
		reqRecieved := false
		for !resSended || !reqRecieved {
			n, addr, err := conn.ReadFrom(rcvBuff)
			if err != nil {
				errRead = err
				return
			}
			if log.LogLvl.Level() <= slog.LevelDebug {
				fmt.Printf("\n%s\n", hex.Dump(rcvBuff[:n]))
			}
			var ok bool
			connAddr, ok = addr.(*net.UDPAddr)
			if !ok {
				continue
			}
			if _, ok := rAddrsMap[addr.String()]; !ok {
				continue
			}
			headers, err := fromBytesHeaders(rcvBuff[:n])
			if err != nil {
				continue
			}
			if headers.type_ == connReq {
				writeCh <- connectResponse(headers.id[:])
				resSended = true
			}
			if headers.type_ == connRes && headers.id == id(req) {
				reqRecieved = true
			}
			if headers.type_ == connClose {
				connAddr = nil
				return
			}
		}
	}()

	b := req.Bytes()
	for i := 0; ; i = (i + 1) % len(rAddrs) {
		n, err := conn.WriteTo(b, rAddrs[i])
		if err != nil {
			errDeadLine := conn.SetReadDeadline(time.Now())
			if errDeadLine != nil {
				panic(errDeadLine)
			}
			for range <-writeCh {
			}
			return nil, err
		}
		if n != len(b) {
			errDeadLine := conn.SetReadDeadline(time.Now())
			if errDeadLine != nil {
				panic(errDeadLine)
			}
			for range <-writeCh {
			}
			return nil, errors.New("error in sending data")
		}
		select {
		case <-time.After(time.Millisecond * 100):
			b = req.Bytes()

		case res, ok := <-writeCh:
			if !ok {
				return connAddr, errRead
			}
			b = res.Bytes()
		}
	}
}

func Disconnect(conn *net.UDPConn, contactAddr net.Addr) error {
	_, err := conn.WriteTo(connectClose(makeId()).Bytes(), contactAddr)
	return err
}

func ReadIncome(conn *net.UDPConn, contactAddr net.Addr, printCh chan<- string, writeCh chan<- []byte, lastPacketTimeCh chan<- time.Time) error {
	rcvBuff := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFrom(rcvBuff)
		if err != nil {
			return err
		}
		if log.LogLvl.Level() <= slog.LevelDebug {
			fmt.Printf("\n%s\n", hex.Dump(rcvBuff[:n]))
		}
		if addr.String() != contactAddr.String() {
			log.Logger.Warn(fmt.Sprintf("recieved data from unexpected adress %s", addr))
			continue
		}
		lastPacketTimeCh <- time.Now()
		if n < 17 {
			log.Logger.Error("invalid data")
			continue
		}
		h, err := fromBytesHeaders(rcvBuff[:17])
		if err != nil {
			log.Logger.Error(err.Error())
			continue
		}
		switch h.type_ {
		case connReq:
			writeCh <- connectResponse(h.id[:]).Bytes()

		case msgReq:
			msg, err := fromBytesMessage(rcvBuff[17:n])
			if err != nil {
				log.Logger.Error(err.Error())
				continue
			}
			printCh <- fmt.Sprintf("%s they: %s\r\n", msg.Time.Format("2006-01-02 15:04:05"), msg.Str)
			writeCh <- messageResponse(h.id[:]).Bytes()

		case connClose:
			return nil
		}
	}
}

func WriteOut(ctx context.Context, conn *net.UDPConn, rAddr *net.UDPAddr, ch <-chan []byte) error {
	timer := time.NewTimer(time.Second)
	for {
		select {
		case <-ctx.Done():
			return nil

		case b := <-ch:
			n, err := conn.WriteTo(b, rAddr)
			if err != nil {
				return err
			}
			if n != len(b) {
				return errors.New("invalid data")
			}
			timer.Reset(time.Second)

		case <-timer.C:
			b := connectRequest(makeId()).Bytes()
			n, err := conn.WriteTo(b, rAddr)
			if err != nil {
				return err
			}
			if n != len(b) {
				return errors.New("invalid data")
			}
		}
	}
}
