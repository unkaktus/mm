// mm.go - move mail from POP3 to Maildir.
//
// To the extent possible under law, Ivan Markin waived all copyright
// and related or neighboring rights to this module of mm, using the creative
// commons "cc0" public domain dedication. See LICENSE or
// <http://creativecommons.org/publicdomain/zero/1.0/> for full details.

package main

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/textproto"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/net/proxy"
)

type POP3Conn struct {
	conn *textproto.Conn
}

type Dialer interface {
	Dial(network, address string) (net.Conn, error)
}

func ParseResponseLine(input string) (ok bool, msg string, err error) {
	s := strings.SplitN(input, " ", 2)
	switch s[0] {
	case "+OK":
		ok = true
	case "-ERR":
		ok = false
	default:
		return false, "", fmt.Errorf("Malformed response status: %s", s[0])
	}
	if len(s) == 2 {
		msg = s[1]
	}
	return ok, msg, nil
}

func (pc *POP3Conn) Cmd(format string, args ...interface{}) (string, error) {
	c := pc.conn
	id, err := c.Cmd(format, args...)
	if err != nil {
		return "", err
	}
	c.StartResponse(id)
	defer c.EndResponse(id)
	line, err := c.ReadLine()
	if err != nil {
		return "", nil
	}
	ok, rmsg, err := ParseResponseLine(line)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%s", rmsg)
	}
	return rmsg, nil
}

func (pc *POP3Conn) CmdMulti(format string, args ...interface{}) (string, []byte, error) {
	c := pc.conn
	id, err := c.Cmd(format, args...)
	if err != nil {
		return "", nil, err
	}
	c.StartResponse(id)
	defer c.EndResponse(id)
	line, err := c.ReadLine()
	if err != nil {
		return "", nil, err
	}
	ok, rmsg, err := ParseResponseLine(line)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, fmt.Errorf("%s", rmsg)
	}
	data, err := c.ReadDotBytes()
	if err != nil {
		return "", nil, err
	}
	return rmsg, data, nil
}

func NewPOP3Conn(rwc io.ReadWriteCloser) *POP3Conn {
	pc := &POP3Conn{}
	pc.conn = textproto.NewConn(rwc)
	return pc

}

func SaveToMaildir(mdpath string, msg []byte) error {
	u := make([]byte, 16)
	_, err := rand.Read(u)
	if err != nil {
		return err
	}
	unique := fmt.Sprintf("%x", u)
	tmpFile := filepath.Join(mdpath, "tmp", unique)
	err = ioutil.WriteFile(tmpFile, msg, 0600)
	if err != nil {
		return err
	}
	newFile := filepath.Join(mdpath, "new", unique)
	err = os.Rename(tmpFile, newFile)
	if err != nil {
		return nil
	}
	return nil
}

type Config struct {
	Username      string
	Password      string
	MaildirPath   string
	TLSServerName string
	ServerAddress string
	ProxyAddress  string
	DisableTLS    bool
}

func main() {
	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	cfgpath := filepath.Join(usr.HomeDir, ".mm.conf")
	flag.Parse()
	if len(flag.Args()) == 1 {
		cfgpath = flag.Args()[0]
	}
	var cfg Config
	cfgdata, err := ioutil.ReadFile(cfgpath)
	if err != nil {
		log.Fatal(err)
	}
	err = json.Unmarshal(cfgdata, &cfg)
	if err != nil {
		log.Fatal(err)
	}
	var dialer Dialer
	dialer = &net.Dialer{}
	if cfg.ProxyAddress != "" {
		var err error
		dialer, err = proxy.SOCKS5("tcp", cfg.ProxyAddress, nil, proxy.Direct)
		if err != nil {
			log.Fatal(err)
		}
	}
	var conn net.Conn
	conn, err = dialer.Dial("tcp", cfg.ServerAddress)
	if err != nil {
		log.Fatal(err)
	}
	if !cfg.DisableTLS {
		tlsConfig := &tls.Config{ServerName: cfg.TLSServerName}
		tlsConn := tls.Client(conn, tlsConfig)
		if err != nil {
			log.Fatal(err)
		}
		conn = tlsConn
	}
	buf := make([]byte, 255)
	n, err := conn.Read(buf)
	if err != nil {
		log.Fatal(err)
	}
	ok, msg, err := ParseResponseLine(string(buf[:n]))
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		log.Fatalf("Server returned error: %s", msg)
	}

	popConn := NewPOP3Conn(conn)

	line, err := popConn.Cmd("USER %s", cfg.Username)
	if err != nil {
		log.Fatal(err)
	}
	line, err = popConn.Cmd("PASS %s", cfg.Password)
	log.Printf("\"%s\"\n", line)
	if err != nil {
		log.Fatal(err)
	}

	line, err = popConn.Cmd("STAT")
	if err != nil {
		log.Fatal(err)
	}
	s := strings.Split(line, " ")
	if len(s) != 2 {
		log.Fatalf("Malformed STAT response: %s", line)
	}
	nmsg, err := strconv.Atoi(s[0])
	if err != nil {
		log.Fatal(err)
	}
	boxsize, err := strconv.Atoi(s[1])
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("There are %d messages of total size %d bytes", nmsg, boxsize)

	for i := 1; i <= nmsg; i++ {
		line, data, err := popConn.CmdMulti("RETR %d", i)
		if err != nil {
			log.Fatal(err)
		}
		s := strings.SplitN(line, " ", 2)
		msgsize, err := strconv.Atoi(s[0])
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Fetching message %d/%d (%d bytes)", i, nmsg, msgsize)
		err = SaveToMaildir(cfg.MaildirPath, data)
		if err != nil {
			log.Fatal(err)
		}

		line, err = popConn.Cmd("DELE %d", i)
		if err != nil {
			log.Fatal(err)
		}
	}

	line, err = popConn.Cmd("QUIT")
	log.Printf("\"%s\"", line)
	if err != nil {
		log.Fatal(err)
	}

	conn.Close()

}
