// mm.go - move mail from POP3 to Maildir.
//
// To the extent possible under law, Ivan Markin waived all copyright
// and related or neighboring rights to this module of mm, using the creative
// commons "cc0" public domain dedication. See LICENSE or
// <http://creativecommons.org/publicdomain/zero/1.0/> for full details.

package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/net/proxy"
)

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
