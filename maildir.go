// maildir.go
//
// To the extent possible under law, Ivan Markin waived all copyright
// and related or neighboring rights to this module of mm, using the creative
// commons "cc0" public domain dedication. See LICENSE or
// <http://creativecommons.org/publicdomain/zero/1.0/> for full details.

package main

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

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
